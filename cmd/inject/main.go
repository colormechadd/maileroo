// inject — send raw .eml files to the Maileroo SMTP server for testing.
//
// Usage:
//
//	go run ./cmd/inject email.eml
//	go run ./cmd/inject *.eml
//	go run ./cmd/inject --from sender@example.com --to rcpt@yourdomain.com email.eml
//	go run ./cmd/inject --host localhost --port 2525 email.eml
//
// The script reads From/To from email headers automatically if not overridden.
// The recipient must match a mailbox address mapping in the database.
package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

const workers = 5

func main() {
	from := flag.String("from", "", "Override MAIL FROM address")
	to := flag.String("to", "", "Override RCPT TO address (comma-separated)")
	host := flag.String("host", "localhost", "SMTP host")
	port := flag.String("port", "2525", "SMTP port")
	dryRun := flag.Bool("dry-run", false, "Parse and display without sending")
	flag.Parse()

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "usage: inject [flags] <email.eml> ...")
		flag.PrintDefaults()
		os.Exit(1)
	}

	var (
		ok      atomic.Int64
		errCount atomic.Int64
		wg      sync.WaitGroup
		jobs    = make(chan string, len(files))
	)

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				fmt.Printf("\n%s\n", path)
				if err := inject(path, *from, *to, *host, *port, *dryRun); err != nil {
					fmt.Printf("  ERROR: %v\n", err)
					errCount.Add(1)
				} else {
					ok.Add(1)
				}
			}
		}()
	}

	for _, path := range files {
		jobs <- path
	}
	close(jobs)
	wg.Wait()

	fmt.Printf("\n%d sent, %d failed\n", ok.Load(), errCount.Load())
	if errCount.Load() > 0 {
		os.Exit(1)
	}
}

func inject(path, fromFlag, toFlag, host, port string, dryRun bool) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return fmt.Errorf("parse failed: %w", err)
	}

	sender := fromFlag
	if sender == "" {
		addrs, err := msg.Header.AddressList("From")
		if err != nil || len(addrs) == 0 {
			return fmt.Errorf("could not determine sender; use --from")
		}
		sender = addrs[0].Address
	}

	var recipients []string
	if toFlag != "" {
		for _, a := range strings.Split(toFlag, ",") {
			if t := strings.TrimSpace(a); t != "" {
				recipients = append(recipients, t)
			}
		}
	} else {
		for _, hdr := range []string{"To", "Cc"} {
			addrs, err := msg.Header.AddressList(hdr)
			if err != nil {
				continue
			}
			for _, a := range addrs {
				recipients = append(recipients, a.Address)
			}
		}
		if len(recipients) == 0 {
			return fmt.Errorf("could not determine recipients; use --to")
		}
	}

	fmt.Printf("  From:    %s\n", sender)
	fmt.Printf("  To:      %s\n", strings.Join(recipients, ", "))
	fmt.Printf("  Subject: %s\n", msg.Header.Get("Subject"))
	fmt.Printf("  Size:    %d bytes\n", len(raw))

	if dryRun {
		fmt.Println("  [dry-run, not sending]")
		return nil
	}

	addr := net.JoinHostPort(host, port)
	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer c.Close()

	// Use STARTTLS if advertised, but skip certificate verification (dev server).
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{InsecureSkipVerify: true}); err != nil { //nolint:gosec
			return fmt.Errorf("starttls: %w", err)
		}
	}

	if err := c.Mail(sender); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, r := range recipients {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}
	if err := c.Quit(); err != nil {
		return fmt.Errorf("quit: %w", err)
	}

	fmt.Println("  OK")
	return nil
}
