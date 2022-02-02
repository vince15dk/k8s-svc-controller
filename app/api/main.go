package main

import (
	"context"
	"github.com/ardanlabs/conf"
	"github.com/pkg/errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// build is the git version of this program. It is set using build flags in build
var build = "develop"

// TLS Cert, Key file path underlying TLS communication for webhook
const (
	tlsCertPath = "/certs/webhook.crt"
	tlsKeyPATH = "/certs/webhook-key.pem"
)

// main execution
func main(){
	log := log.New(os.Stdout, "k8s-svc-controller : ", log.LstdFlags|log.Lmicroseconds|log.Lshortfile)
	if err := run(log); err != nil{
		log.Println("main: error", err)
		os.Exit(1)
	}
}

func run(log *log.Logger) error{

	// =========================================================================
	// Configuration
	var cfg struct{
		conf.Version
		Web struct{
			APIHost string `conf:"default:0.0.0.0:443"`
			ReadTimeout time.Duration `conf:"default:5s"`
			WriteTimeout time.Duration `conf:"default:5s"`
			ShutdownTimeout time.Duration `conf:"default:5s"`
		}
	}

	cfg.Version.SVN = build
	cfg.Version.Desc = "copyright v1.0.0"

	// parse the arguments from conf
	if err := conf.Parse(os.Args[1:], "k8s-svc-controller", &cfg); err != nil{
		switch err{
		case conf.ErrHelpWanted:
			usage, err := conf.Usage("k8s-svc-controller", &cfg)
			if err != nil{
				return errors.Wrap(err, "generating config usage")
			}
			log.Println(usage)
			return nil
		case conf.ErrVersionWanted:
			version, err := conf.VersionString("k8s-svc-controller", &cfg)
			if err != nil{
				return errors.Wrap(err, "generating config version")
			}
			log.Println(version)
			return nil
		}
		return errors.Wrap(err, "parsing config")
	}

	defer log.Println("main: Completed")

	out, err := conf.String(&cfg)
	if err != nil{
		return errors.Wrap(err, "generating config for output")
	}
	log.Printf("main: Config :\n%v\n", out)

	// =========================================================================
	// Start API Service
	log.Println("main: Initializing API support")

	// Make a channel to listen for an interrupt or terminate signal from the OS.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	api := http.Server{
		Addr: cfg.Web.APIHost,
		//Handler: handlers.API(build, shutdown, log),
		ReadTimeout: cfg.Web.ReadTimeout,
		WriteTimeout: cfg.Web.WriteTimeout,
	}

	// Make a channel to listen for errors coming from the listener. Use a
	// buffered channel so the goroutine can exit
	serverErrors := make(chan error, 1)

	// Start the service listening for requests.
	go func() {
		log.Printf("main: API listening on %s", api.Addr)
		serverErrors <- api.ListenAndServeTLS(tlsCertPath, tlsKeyPATH)
	}()

	// =========================================================================
	// Shutdown

	// Blocking main and waiting for shutdown.
	select{
	case err := <-serverErrors:
		return errors.Wrap(err, "server error")

	case sig := <-shutdown:
		log.Printf("main: %v : Start shutdown", sig)

		// Give outstanding requests a deadline for completion
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Web.ShutdownTimeout)
		defer cancel()


		// gracefully shutdown
		if err := api.Shutdown(ctx); err != nil{
			// when timeout occurs, hits this. safely timeout is long enough
			api.Close()
			return errors.Wrap(err, "could not stop server gracefully")
		}
	}
	return nil
}
