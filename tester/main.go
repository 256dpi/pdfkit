package main

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/256dpi/pdfkit"
)

func main() {
	// create printer
	printer, err := pdfkit.CreatePrinter(pdfkit.Config{
		QueueSize:   20,
		Concurrency: 4,
		ServerPort:  1234,
	})
	if err != nil {
		panic(err)
	}

	// clear output directory
	err = os.RemoveAll("out")
	if err != nil {
		panic(err)
	}

	// create output directory
	err = os.MkdirAll("out", 0755)
	if err != nil {
		panic(err)
	}

	// run workers
	var num int32
	var wg sync.WaitGroup
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()

			for {
				// get next number
				n := atomic.AddInt32(&num, 1)
				if n > 20 {
					return
				}

				// print file
				file := []byte(fmt.Sprintf("<h1>%d</h1>", n))
				bytes, err := printer.PrintFile(file, 5*time.Second, nil)
				if err != nil {
					fmt.Printf("Error: %s\n", err)
					return
				}

				// write file
				err = os.WriteFile(fmt.Sprintf("out/%d.pdf", n), bytes, 0666)
				if err != nil {
					fmt.Printf("Error: %s\n", err)
					return
				}

				// print success
				fmt.Printf("Printed %d\n", n)
			}
		}()
	}

	wg.Wait()
}
