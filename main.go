package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

const (
	elapseTickPeriod = 100 * time.Millisecond
)

type statistic struct {
	Time           time.Time
	TotalRequest   int64
	SuccessRequest int64
	FailRequest    int64
	Elapse         time.Duration
}

// TODO: Get N parameter.
// TODO: Target TPS.
func main() {
	cmd := cobra.Command{
		Use: "load",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Args", args)

			flags := cmd.Flags()

			nWorkers, err := flags.GetInt("workers")
			if err != nil {
				return err
			}

			ctx := context.Background()
			ctx, cancel := context.WithCancel(ctx)
			go func() {
				c := make(chan os.Signal, 1)
				signal.Notify(c, syscall.SIGTERM, syscall.SIGKILL, syscall.SIGINT)

				_ = <-c

				log.Println("Interrupt")
				cancel()
			}()

			startLoader(ctx, nWorkers)
			return nil
		},
	}

	cmd.Flags().IntP("workers", "w", 10, "Number of workers")

	if err := cmd.Execute(); err != nil {
		log.Panic(err)
	}
}

func startLoader(ctx context.Context, workers int) {
	sChan := make(chan int, 100)
	fChan := make(chan int, 100)
	cmdChan := make(chan string, 100)

	statChan := tpsCalculator(ctx, sChan, fChan)

	go addQueue(ctx, cmdChan)
	go printTPS(ctx, statChan)
	startWorkers(ctx, workers, cmdChan, sChan, fChan)
}

func tpsCalculator(ctx context.Context, successChan, failChan <-chan int) chan statistic {
	tpsChan := make(chan statistic, 1)

	go func() {
		stat := statistic{}

		for {
			select {
			case n := <-successChan:
				stat.SuccessRequest += int64(n)
				stat.TotalRequest += int64(n)
			case n := <-failChan:
				stat.FailRequest += int64(n)
				stat.TotalRequest += int64(n)
			case <-time.Tick(elapseTickPeriod):
				stat.Time = time.Now()
				stat.Elapse += elapseTickPeriod
				tpsChan <- stat
			case <-ctx.Done():
				break
			}
		}
	}()

	return tpsChan
}

func addQueue(ctx context.Context, cmdChan chan<- string) {
	for {
		select {
		case <-time.Tick(1 * time.Millisecond):
			cmdChan <- "./script/test.sh"
		case <-ctx.Done():
			break
		}
	}
}

func printTPS(ctx context.Context, statChan <-chan statistic) {
	for {
		select {
		case stat := <-statChan:
			log.Printf("Req %d/%d, TPS: %f\n", stat.SuccessRequest, stat.TotalRequest, float64(stat.SuccessRequest)/stat.Elapse.Seconds())
		case <-ctx.Done():
			break
		}
	}
}

func startWorkers(ctx context.Context, n int, cmdChan <-chan string, successChan, failChan chan<- int) {
	log.Println("Start Worker")

	wg := sync.WaitGroup{}
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(idx int) {
			log.Println("Start worker", idx)
			defer log.Println("Stop worker")
			defer wg.Done()

			for {
				select {
				case cmd, ok := <-cmdChan:
					if !ok {
						return
					}

					c := exec.Command(cmd)
					err := c.Run()
					if err != nil {
						failChan <- 1
					} else {
						successChan <- 1
					}
				case <-ctx.Done():
					return
				}
			}
		}(i)
	}

	wg.Wait()
}

// TODO: Load wasm binary by code.
// TODO: Call wasm contract. Require heavy load contract.
// TODO: Report output.
