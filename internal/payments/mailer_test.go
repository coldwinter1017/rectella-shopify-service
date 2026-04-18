package payments

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockSMTPServer is a tiny in-process SMTP listener sufficient for a
// happy-path test. Accepts PLAIN auth, captures the DATA body, and
// returns it via the `received` channel.
type mockSMTPServer struct {
	listener net.Listener
	host     string
	port     int
	received chan mockReceived
	wg       sync.WaitGroup
}

type mockReceived struct {
	from string
	to   []string
	body string
}

func startMockSMTP(t *testing.T) *mockSMTPServer {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	host, portStr, _ := net.SplitHostPort(l.Addr().String())
	var port int
	for _, r := range portStr {
		port = port*10 + int(r-'0')
	}

	srv := &mockSMTPServer{
		listener: l,
		host:     host,
		port:     port,
		received: make(chan mockReceived, 4),
	}
	srv.wg.Add(1)
	go srv.acceptLoop()
	return srv
}

func (s *mockSMTPServer) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *mockSMTPServer) handle(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	writeLine := func(line string) {
		_, _ = io.WriteString(conn, line+"\r\n")
	}
	writeLine("220 mock.smtp ready")

	var rec mockReceived
	var inData bool
	var dataBuf strings.Builder

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if inData {
			if line == "." {
				inData = false
				rec.body = dataBuf.String()
				writeLine("250 OK queued")
				s.received <- rec
				continue
			}
			dataBuf.WriteString(line + "\n")
			continue
		}
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			writeLine("250-mock.smtp")
			writeLine("250 AUTH PLAIN")
		case strings.HasPrefix(upper, "AUTH"):
			writeLine("235 2.7.0 Authentication successful")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			rec.from = strings.TrimSpace(strings.TrimPrefix(line[10:], " "))
			rec.from = strings.Trim(rec.from, "<>")
			writeLine("250 OK")
		case strings.HasPrefix(upper, "RCPT TO:"):
			addr := strings.TrimSpace(strings.TrimPrefix(line[8:], " "))
			addr = strings.Trim(addr, "<>")
			rec.to = append(rec.to, addr)
			writeLine("250 OK")
		case upper == "DATA":
			writeLine("354 go")
			inData = true
			dataBuf.Reset()
		case upper == "QUIT":
			writeLine("221 bye")
			return
		case upper == "RSET":
			writeLine("250 OK")
		case upper == "NOOP":
			writeLine("250 OK")
		default:
			writeLine("250 OK")
		}
	}
}

func (s *mockSMTPServer) close() {
	_ = s.listener.Close()
	s.wg.Wait()
}

func TestMailer_Send_HappyPath(t *testing.T) {
	srv := startMockSMTP(t)
	defer srv.close()

	m := NewMailer(MailerConfig{
		Host:     srv.host,
		Port:     srv.port,
		Username: "user",
		Password: "pw",
		From:     "reports@example.com",
		UseTLS:   false,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	att := &Attachment{
		Filename:    "report.csv",
		ContentType: "text/csv",
		Body:        []byte("header\nrow\n"),
	}
	err := m.Send(ctx, []string{"creditcontrol@example.com"}, "Daily Cash Receipts", "Totals enclosed.", att)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case rec := <-srv.received:
		if rec.from != "reports@example.com" {
			t.Errorf("from = %q", rec.from)
		}
		if len(rec.to) != 1 || rec.to[0] != "creditcontrol@example.com" {
			t.Errorf("to = %v", rec.to)
		}
		if !strings.Contains(rec.body, "Subject: Daily Cash Receipts") {
			t.Error("missing subject header")
		}
		if !strings.Contains(rec.body, "Totals enclosed.") {
			t.Error("missing text body")
		}
		if !strings.Contains(rec.body, "report.csv") {
			t.Error("missing attachment filename")
		}
		if !strings.Contains(rec.body, "multipart/mixed") {
			t.Error("missing multipart header")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no message received")
	}
}

func TestMailer_Send_Validation(t *testing.T) {
	m := NewMailer(MailerConfig{Host: "h", Port: 1, From: "a@b"})
	err := m.Send(context.Background(), nil, "s", "b", nil)
	if err == nil {
		t.Fatal("want error for empty recipients")
	}
}
