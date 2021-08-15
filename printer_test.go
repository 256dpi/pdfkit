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
		ServerReporter: func(err error) {
			panic(err)
		},
	})
	assert.NoError(t, err)
	assert.NotNil(t, printer)

	defer printer.Close()

	pdf, err := printer.PrintURL("https://sternenbauer.com", time.Minute)
	assert.NoError(t, err)
	assert.NotEmpty(t, pdf)

	write("page.pdf", pdf)
}

func TestPrinterPrintFile(t *testing.T) {
	printer, err := CreatePrinter(Config{
		QueueSize:   1,
		Concurrency: 1,
		ServerReporter: func(err error) {
			panic(err)
		},
	})
	assert.NoError(t, err)
	assert.NotNil(t, printer)

	defer printer.Close()

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

	write("file.pdf", pdf)
}

func TestPrinterPrintFileAssets(t *testing.T) {
	assets := map[string][]byte{}

	printer, err := CreatePrinter(Config{
		QueueSize:   1,
		Concurrency: 1,
		ServerReporter: func(err error) {
			panic(err)
		},
	})
	assert.NoError(t, err)
	assert.NotNil(t, printer)

	defer printer.Close()

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

	write("assets.pdf", pdf)
}

func BenchmarkPrinter(b *testing.B) {
	printer, err := CreatePrinter(Config{
		QueueSize:   runtime.GOMAXPROCS(0),
		Concurrency: runtime.GOMAXPROCS(0),
		ServerReporter: func(err error) {
			panic(err)
		},
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
