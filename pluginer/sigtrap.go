package pluginer

import (
	"log"
	"os"
	"os/signal"
	"sync"
)

var (
	// shutdownCallbacksOnce ensures that shutdown callbacks
	// for all instances are only executed once.
	shutdownCallbacksOnce sync.Once
)

// 处理通用平台的 SIGINT (中断信号，通常是 Ctrl+C),不支持Uinx系统的其他信号
func TrapSignalsCrossPlatform() {
	go func() {
		shutdown := make(chan os.Signal, 1)
		signal.Notify(shutdown, os.Interrupt)

		for i := 0; true; i++ {
			<-shutdown

			if i > 0 {
				log.Println("[INFO] SIGINT: Force quit")
				for _, f := range OnProcessExit {
					f() // important cleanup actions only
				}
				os.Exit(2)
			}

			log.Println("[INFO] SIGINT: Shutting down")

			for _, f := range OnProcessExit {
				f()
			}

			go func() {
				os.Exit(executeShutdownCallbacks("SIGINT"))
			}()
		}
	}()
}

func executeShutdownCallbacks(signame string) (exitCode int) {
	shutdownCallbacksOnce.Do(func() {
		// execute third-party shutdown hooks
		// EmitEvent(ShutdownEvent, signame)

		errs := allShutdownCallbacks()
		if len(errs) > 0 {
			for _, err := range errs {
				log.Printf("[ERROR] %s shutdown: %v", signame, err)
			}
			exitCode = 4
		}
	})
	return
}

func allShutdownCallbacks() []error {
	var errs []error
	instancesMu.Lock()
	for _, inst := range instances {
		errs = append(errs, inst.ShutdownCallbacks()...)
	}
	instancesMu.Unlock()
	return errs
}
