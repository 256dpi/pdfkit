package pdfkit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/256dpi/serve"
	"github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// ErrQueueTimeout is returned if a job timed out while queueing.
var ErrQueueTimeout = errors.New("queue timeout")

// ErrProcessTimeout is returned if a job timed out while processing.
var ErrProcessTimeout = errors.New("process timeout")

// LogError may be returned if the processing failed due to a page error.
type LogError struct {
	Lines []string
}

func (e *LogError) Error() string {
	return strings.Join(e.Lines, "\n")
}

type job struct {
	context context.Context
	done    chan struct{}
	secret  string
	url     string
	file    []byte
	assets  map[string][]byte
	result  []byte
	error   error
}

// Config defines a printer configuration.
type Config struct {
	QueueSize      int
	ServerPort     int
	ServerReporter func(error)
}

// Printer prints web pages as PDFs.
type Printer struct {
	counter int64
	context context.Context
	cancel  func()
	queue   chan *job
	socket  net.Listener
	addr    string
	group   sync.WaitGroup
	jobs    sync.Map
}

// CreatePrinter will create a new printer.
func CreatePrinter(config Config) (*Printer, error) {
	// check config
	if config.QueueSize < 0 {
		return nil, fmt.Errorf("negative queue size")
	} else if config.ServerPort < 0 {
		return nil, fmt.Errorf("negative server port")
	}

	// prepare context
	ctx, cancel := chromedp.NewContext(context.Background())

	// ensure cleanup
	var success bool
	defer func() {
		if !success {
			_ = chromedp.Cancel(ctx)
			cancel()
		}
	}()

	// allocate browser
	err := chromedp.Run(ctx)
	if err != nil {
		return nil, err
	}

	// create socket
	socket, err := net.Listen("tcp", ":"+strconv.Itoa(config.ServerPort))
	if err != nil {
		return nil, err
	}

	// get port
	_, port, err := net.SplitHostPort(socket.Addr().String())
	if err != nil {
		return nil, err
	}

	// prepare printer
	p := &Printer{
		context: ctx,
		cancel:  cancel,
		socket:  socket,
		addr:    "http://0.0.0.0:" + port,
		queue:   make(chan *job, config.QueueSize),
	}

	// run server
	go func() {
		for {
			err := http.Serve(socket, http.HandlerFunc(p.handler))
			if err != nil && errors.Is(err, net.ErrClosed) {
				return
			} else if err != nil && config.ServerReporter != nil {
				config.ServerReporter(err)
			}
		}
	}()

	// run printer
	p.group.Add(1)
	go p.run()

	// set flag
	success = true

	return p, nil
}

// PrintURL will print the provided URL as a PDF.
func (p *Printer) PrintURL(url string, timeout time.Duration) ([]byte, error) {
	// wrap context
	ctx, cancel := context.WithTimeout(p.context, timeout)
	defer cancel()

	return p.process(&job{
		context: ctx,
		url:     url,
	})
}

// PrintFile will print the provided file and its assets as a PDF.
func (p *Printer) PrintFile(file []byte, timeout time.Duration, assets map[string][]byte) ([]byte, error) {
	// wrap context
	ctx, cancel := context.WithTimeout(p.context, timeout)
	defer cancel()

	return p.process(&job{
		context: ctx,
		file:    file,
		assets:  assets,
	})
}

func (p *Printer) process(job *job) ([]byte, error) {
	// create done
	job.done = make(chan struct{})

	// read secret
	secret := make([]byte, 16)
	_, err := rand.Read(secret)
	if err != nil {
		return nil, err
	}

	// set secret
	job.secret = hex.EncodeToString(secret)

	// queue job
	select {
	case p.queue <- job:
	case <-job.context.Done():
		return nil, ErrQueueTimeout
	}

	// await finish
	select {
	case <-job.done:
	case <-job.context.Done():
		return nil, ErrProcessTimeout
	}

	return job.result, job.error
}

func (p *Printer) run() {
	// ensure done
	defer p.group.Done()

	for {
		// get job
		job, ok := <-p.queue
		if !ok {
			return
		}

		// get id
		id := strconv.Itoa(int(atomic.AddInt64(&p.counter, 1)))

		// store job
		p.jobs.Store(id, job)

		// print url or file
		if job.url != "" {
			job.result, job.error = p.print(job.context, job.url, "")
		} else {
			data := id + "," + job.secret
			job.result, job.error = p.print(job.context, p.addr, data)
		}

		// delete job
		p.jobs.Delete(id)

		// signal done
		close(job.done)
	}
}

func (p *Printer) print(ctx context.Context, url, data string) ([]byte, error) {
	// create sub context
	ctx, cancel := chromedp.NewContext(ctx)
	defer cancel()
	defer func() {
		_ = chromedp.Cancel(ctx)
	}()

	// collect errors
	var logErrors []string
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		if ev, ok := ev.(*log.EventEntryAdded); ok {
			if ev.Entry.Level == log.LevelError {
				logErrors = append(logErrors, fmt.Sprintf("%s (%s)", ev.Entry.Text, ev.Entry.URL))
			}
		}
	})

	// render pdf
	var buf []byte
	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			if data == "" {
				return nil
			}
			return network.SetCookie("pdfkit", data).
				WithURL(p.addr).
				WithHTTPOnly(true).
				Do(ctx)
		}),
		chromedp.Navigate(url),
		chromedp.WaitVisible("body"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			buf, _, err = page.PrintToPDF().
				WithLandscape(false).
				WithDisplayHeaderFooter(false).
				WithPrintBackground(true).
				WithScale(1).
				WithPaperWidth(8.27).   // A4 (210mm)
				WithPaperHeight(11.69). // A4 (297mm)
				WithPreferCSSPageSize(true).
				Do(ctx)
			return err
		}),
	)
	if err != nil {
		return nil, err
	}

	// handle log errors
	if len(logErrors) > 0 {
		return nil, &LogError{Lines: logErrors}
	}

	// cancel context
	err = chromedp.Cancel(ctx)
	if err != nil {
		return nil, err
	}

	return buf, nil
}

func (p *Printer) handler(w http.ResponseWriter, r *http.Request) {
	// get data
	cookie, _ := r.Cookie("pdfkit")
	if cookie == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// split data
	data := strings.Split(cookie.Value, ",")
	if len(data) != 2 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// get value
	value, ok := p.jobs.Load(data[0])
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// get job
	job := value.(*job)

	// check secret
	if data[1] != job.secret {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// get path
	path := strings.Trim(r.URL.Path, "/")

	// write file if index
	if path == "" {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(job.file)
		return
	}

	// get asset
	if job.assets == nil || job.assets[path] == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// write asset
	w.Header().Set("Content-Type", serve.MimeTypeByExtension(filepath.Ext(path), true))
	_, _ = w.Write(job.assets[path])
}

// Close will close the printer.
func (p *Printer) Close() error {
	// ensure context cancel
	defer p.cancel()

	// close queue
	close(p.queue)

	// await exit
	p.group.Wait()

	// close socket
	err1 := p.socket.Close()

	// cancel context
	err2 := chromedp.Cancel(p.context)

	// check error
	if err1 != nil {
		return err1
	} else if err2 != nil {
		return err2
	}

	return nil
}
