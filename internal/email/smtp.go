package email

import (
	"crypto/rand"
	"mime"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net/smtp"
	"strings"
	"time"
)


// encodeHeader encodes a header value using RFC 2047 Q-encoding if it contains non-ASCII characters.
func encodeHeader(s string) string {
	for _, r := range s {
		if r > 127 {
			return mime.QEncoding.Encode("utf-8", s)
		}
	}
	return s
}

// SendRequest holds parameters for sending an email.
type SendRequest struct {
	To          []string
	CC          []string
	BCC         []string
	Subject     string
	Body        string
	HTMLBody    string
	InReplyTo   string
	References  string
	MessageID   string
	FromAddress string
}

// Send sends an email via SMTP.
func Send(cfg AccountConfig, req SendRequest) error {
	from := req.FromAddress
	if from == "" {
		from = cfg.FromAddress
	}

	msgID := req.MessageID
	if msgID == "" {
		msgID = fmt.Sprintf("<%d@theseus>", time.Now().UnixNano())
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("From: %s\r\n", from))
	sb.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(req.To, ", ")))
	if len(req.CC) > 0 {
		sb.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(req.CC, ", ")))
	}
	sb.WriteString(fmt.Sprintf("Subject: %s\r\n", req.Subject))
	sb.WriteString(fmt.Sprintf("Message-ID: %s\r\n", msgID))
	if req.InReplyTo != "" {
		sb.WriteString(fmt.Sprintf("In-Reply-To: %s\r\n", req.InReplyTo))
	}
	if req.References != "" {
		sb.WriteString(fmt.Sprintf("References: %s\r\n", req.References))
	}
	sb.WriteString("MIME-Version: 1.0\r\n")

	if req.HTMLBody != "" {
		// Generate a random MIME boundary to prevent injection
		boundaryBytes := make([]byte, 12)
		if _, err := rand.Read(boundaryBytes); err != nil {
			return fmt.Errorf("generate boundary: %w", err)
		}
		boundary := "theseus_" + hex.EncodeToString(boundaryBytes)
		sb.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=%s\r\n\r\n", boundary))
		sb.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		sb.WriteString(req.Body + "\r\n")
		sb.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		sb.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
		sb.WriteString(req.HTMLBody + "\r\n")
		sb.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	} else {
		sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		sb.WriteString(req.Body)
	}

	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	auth := smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPassword, cfg.SMTPHost)
	allTo := make([]string, 0, len(req.To)+len(req.CC)+len(req.BCC))
	allTo = append(allTo, req.To...)
	allTo = append(allTo, req.CC...)
	allTo = append(allTo, req.BCC...)

	if cfg.SMTPPort == 465 {
		return sendTLS(addr, cfg.SMTPHost, auth, from, allTo, []byte(sb.String()))
	}
	return smtp.SendMail(addr, auth, from, allTo, []byte(sb.String()))
}

func sendTLS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Auth(auth); err != nil {
		return err
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	return w.Close()
}
