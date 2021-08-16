package pdfkit

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var pdfDir = flag.String("test.pdfDir", "", "")

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}

func TestPrinterPrintURL(t *testing.T) {
	printer, err := CreatePrinter(Config{
		QueueSize:   1,
		Concurrency: 1,
		ServerPort:  1337,
	})
	assert.NoError(t, err)
	assert.NotNil(t, printer)

	pdf, err := printer.PrintURL("https://sternenbauer.com", time.Minute)
	assert.NoError(t, err)
	assert.NotEmpty(t, pdf)

	err = printer.Close()
	assert.NoError(t, err)

	write("page.pdf", pdf)
}

func TestPrinterPrintFile(t *testing.T) {
	printer, err := CreatePrinter(Config{
		QueueSize:   1,
		Concurrency: 1,
		ServerPort:  1337,
	})
	assert.NoError(t, err)
	assert.NotNil(t, printer)

	pdf, err := printer.PrintFile([]byte(`
		<html>
		<head>
			<style>
				@page {
					size: A5 landscape;
					margin: 0;
				}
			</style>
		</head>
		<body>
			<h1>Hello World!</h1>
		</body>
		</html>
	`), time.Minute, nil)
	assert.NoError(t, err)
	assert.NotEmpty(t, pdf)

	err = printer.Close()
	assert.NoError(t, err)

	write("file.pdf", pdf)
}

func TestPrinterPrintFileAssets(t *testing.T) {
	assets := map[string][]byte{}

	printer, err := CreatePrinter(Config{
		QueueSize:   1,
		Concurrency: 1,
		ServerPort:  1337,
	})
	assert.NoError(t, err)
	assert.NotNil(t, printer)

	file := []byte(`
		<html>
		<head></head>
		<body>
			<img width="100" src="foo.svg">
		</body>
		</html>
	`)

	pdf, err := printer.PrintFile(file, time.Minute, assets)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Not Found")
	assert.Contains(t, err.Error(), "foo.svg")
	assert.Empty(t, pdf)

	assets["foo.svg"] = []byte(`<?xml version="1.0"?>
		<svg width="400px" height="400px" viewBox="0 0 400 400" version="1.1" xmlns="http://www.w3.org/2000/svg">
			<circle fill="#0000FF" cx="150" cy="150" r="150"></circle>
			<rect fill="#FF0000" x="150" y="150" width="250" height="250"></rect>
		</svg>
	`)

	pdf, err = printer.PrintFile(file, time.Minute, assets)
	assert.NoError(t, err)
	assert.NotEmpty(t, pdf)

	err = printer.Close()
	assert.NoError(t, err)

	write("assets.pdf", pdf)
}

func TestPrinterClose(t *testing.T) {
	printer, err := CreatePrinter(Config{
		QueueSize:   1,
		Concurrency: 1,
		ServerPort:  1337,
	})
	assert.NoError(t, err)
	assert.NotNil(t, printer)

	done1 := make(chan struct{})
	go func() {
		pdf, err := printer.PrintURL("https://sternenbauer.com", time.Minute)
		assert.NoError(t, err)
		assert.NotEmpty(t, pdf)
		close(done1)
	}()

	time.Sleep(50 * time.Millisecond)
	done2 := make(chan struct{})
	go func() {
		pdf, err := printer.PrintURL("https://sternenbauer.com", time.Minute)
		assert.Error(t, err)
		assert.Empty(t, pdf)
		assert.Equal(t, ErrClosed, err)
		close(done2)
	}()

	time.Sleep(50 * time.Millisecond)
	err = printer.Close()
	assert.NoError(t, err)

	<-done1
	<-done2

	_, err = printer.PrintURL("https://sternenbauer.com", time.Minute)
	assert.Equal(t, ErrClosed, err)

	err = printer.Close()
	assert.Equal(t, ErrClosed, err)
}

func BenchmarkPrinter(b *testing.B) {
	printer, err := CreatePrinter(Config{
		QueueSize:   runtime.GOMAXPROCS(0),
		Concurrency: runtime.GOMAXPROCS(0),
		ServerPort:  1337,
	})
	if err != nil {
		panic(err)
	}
	defer printer.Close()

	file := []byte(`
		<html>
		<head>
			<style>
				@page {
					size: A5 landscape;
					margin: 0;
				}
			</style>
		</head>
		<body>
			<h1>Hello World!</h1>
		</body>
		</html>
	`)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := printer.PrintFile(file, time.Minute, nil)
			if err != nil {
				panic(err)
			}
		}
	})

	b.StopTimer()
}

func write(name string, pdf []byte) {
	if *pdfDir == "" {
		return
	}
	path, err := filepath.Abs(filepath.Join(*pdfDir, name))
	if err != nil {
		panic(err)
	}
	err = os.WriteFile(path, pdf, 0666)
	if err != nil {
		panic(err)
	}
}
