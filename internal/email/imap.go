package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/mail"
)

// Message represents a fetched email message.
type Message struct {
	UID         uint32    `json:"uid"`
	Subject     string    `json:"subject"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	Date        time.Time `json:"date"`
	Body        string    `json:"body"`
	HTMLBody    string    `json:"html_body"`
	Snippet     string    `json:"snippet"`
	IsRead      bool      `json:"is_read"`
	Flags       []string  `json:"flags"`
	MessageID   string    `json:"message_id"`
	InReplyTo   string    `json:"in_reply_to"`
	Folder      string    `json:"folder"`
}

// Attachment represents an email attachment.
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        []byte `json:"-"`
}

// AccountConfig holds IMAP/SMTP connection parameters.
type AccountConfig struct {
	IMAPHost     string
	IMAPPort     int
	IMAPUser     string
	IMAPPassword string
	IMAPStartTLS bool
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	FromAddress  string
}

// IMAPClient wraps go-imap/v2.
type IMAPClient struct {
	cfg    AccountConfig
	client *imapclient.Client
}

// Connect establishes an IMAP connection.
func Connect(cfg AccountConfig) (*IMAPClient, error) {
	addr := fmt.Sprintf("%s:%d", cfg.IMAPHost, cfg.IMAPPort)
	var c *imapclient.Client
	var err error

	if cfg.IMAPPort == 993 || !cfg.IMAPStartTLS {
		tlsCfg := &tls.Config{ServerName: cfg.IMAPHost}
		c, err = imapclient.DialTLS(addr, &imapclient.Options{TLSConfig: tlsCfg})
	} else {
		c, err = imapclient.DialStartTLS(addr, &imapclient.Options{TLSConfig: &tls.Config{ServerName: cfg.IMAPHost}})
	}
	if err != nil {
		return nil, fmt.Errorf("imap connect: %w", err)
	}
	if err := c.Login(cfg.IMAPUser, cfg.IMAPPassword).Wait(); err != nil {
		c.Close()
		return nil, fmt.Errorf("imap login: %w", err)
	}
	return &IMAPClient{cfg: cfg, client: c}, nil
}

// Close closes the IMAP connection.
func (c *IMAPClient) Close() {
	if c.client != nil {
		c.client.Logout().Wait()
		c.client.Close()
	}
}

// ListFolders returns all mailbox folders.
func (c *IMAPClient) ListFolders(ctx context.Context) ([]string, error) {
	mailboxes, err := c.client.List("", "*", nil).Collect()
	if err != nil {
		return nil, err
	}
	var folders []string
	for _, mb := range mailboxes {
		folders = append(folders, mb.Mailbox)
	}
	return folders, nil
}

// ListMessages returns messages from a folder.
func (c *IMAPClient) ListMessages(ctx context.Context, folder string, limit int, unreadOnly bool) ([]*Message, error) {
	if _, err := c.client.Select(folder, nil).Wait(); err != nil {
		return nil, fmt.Errorf("select %s: %w", folder, err)
	}

	var criteria *imap.SearchCriteria
	if unreadOnly {
		criteria = &imap.SearchCriteria{
			NotFlag: []imap.Flag{imap.FlagSeen},
		}
	} else {
		criteria = &imap.SearchCriteria{}
	}

	searchData, err := c.client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, err
	}

	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}
	if limit > 0 && len(uids) > limit {
		uids = uids[len(uids)-limit:]
	}

	seqSet := imap.UIDSetNum(uids...)
	fetchOptions := &imap.FetchOptions{
		Flags:    true,
		Envelope: true,
		UID:      true,
		BodySection: []*imap.FetchItemBodySection{
			{Specifier: imap.PartSpecifierText},
		},
	}

	messages, err := c.client.Fetch(seqSet, fetchOptions).Collect()
	if err != nil {
		return nil, err
	}

	var result []*Message
	for _, msg := range messages {
		m := parseIMAPMessage(msg, folder)
		if m != nil {
			result = append(result, m)
		}
	}
	return result, nil
}

// MarkRead marks messages as read.
func (c *IMAPClient) MarkRead(ctx context.Context, folder string, uids []imap.UID) error {
	if _, err := c.client.Select(folder, nil).Wait(); err != nil {
		return err
	}
	seqSet := imap.UIDSetNum(uids...)
	_, err := c.client.Store(seqSet, &imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagSeen},
	}, nil).Collect()
	return err
}

// DeleteMessages marks messages as deleted and expunges only those specific UIDs.
func (c *IMAPClient) DeleteMessages(ctx context.Context, folder string, uids []imap.UID) error {
	if _, err := c.client.Select(folder, nil).Wait(); err != nil {
		return err
	}
	seqSet := imap.UIDSetNum(uids...)
	if _, err := c.client.Store(seqSet, &imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagDeleted},
	}, nil).Collect(); err != nil {
		return err
	}
	// Use UIDExpunge to expunge only the specific UIDs, not all deleted messages
	_, err := c.client.UIDExpunge(seqSet).Collect()
	return err
}

// MoveMessages moves messages to another folder.
func (c *IMAPClient) MoveMessages(ctx context.Context, folder, dest string, uids []imap.UID) error {
	if _, err := c.client.Select(folder, nil).Wait(); err != nil {
		return err
	}
	seqSet := imap.UIDSetNum(uids...)
	_, err := c.client.Move(seqSet, dest).Wait()
	return err
}

func parseIMAPMessage(msg *imapclient.FetchMessageBuffer, folder string) *Message {
	m := &Message{
		UID:    uint32(msg.UID),
		Folder: folder,
	}
	for _, flag := range msg.Flags {
		m.Flags = append(m.Flags, string(flag))
		if flag == imap.FlagSeen {
			m.IsRead = true
		}
	}
	if env := msg.Envelope; env != nil {
		m.Subject = env.Subject
		m.Date = env.Date
		m.MessageID = env.MessageID
		if len(env.InReplyTo) > 0 { m.InReplyTo = env.InReplyTo[0] }
		if len(env.From) > 0 {
			m.From = formatAddress(env.From[0])
		}
		var tos []string
		for _, addr := range env.To {
			tos = append(tos, formatAddress(addr))
		}
		m.To = strings.Join(tos, ", ")
	}
	for _, section := range msg.BodySection {
		if len(section.Bytes) == 0 {
			continue
		}
		mr, err := mail.CreateReader(strings.NewReader(string(section.Bytes)))
		if err != nil {
			m.Body = string(section.Bytes)
			continue
		}
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			ct := part.Header.Get("Content-Type")
			data, _ := io.ReadAll(part.Body)
			if strings.HasPrefix(ct, "text/plain") && m.Body == "" {
				m.Body = string(data)
			} else if strings.HasPrefix(ct, "text/html") && m.HTMLBody == "" {
				m.HTMLBody = string(data)
			}
		}
	}
	if m.Body == "" && m.HTMLBody != "" {
		m.Body = stripHTMLTags(m.HTMLBody)
	}
	if len(m.Body) > 300 {
		m.Snippet = m.Body[:300]
	} else {
		m.Snippet = m.Body
	}
	return m
}

func formatAddress(addr imap.Address) string {
	if addr.Name != "" {
		return fmt.Sprintf("%s <%s@%s>", addr.Name, addr.Mailbox, addr.Host)
	}
	return fmt.Sprintf("%s@%s", addr.Mailbox, addr.Host)
}

func stripHTMLTags(html string) string {
	var sb strings.Builder
	inTag := false
	for _, r := range html {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
			sb.WriteRune(' ')
		} else if !inTag {
			sb.WriteRune(r)
		}
	}
	return strings.TrimSpace(sb.String())
}
