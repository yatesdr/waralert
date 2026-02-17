package action

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"text/template"

	"waralert/config"
)

// EmailProvider sends email notifications.
type EmailProvider struct {
	cfg   *config.Config
	logFn func(string, ...interface{})
}

// NewEmailProvider creates a new email provider.
func NewEmailProvider(cfg *config.Config, logFn func(string, ...interface{})) *EmailProvider {
	return &EmailProvider{
		cfg:   cfg,
		logFn: logFn,
	}
}

// Type returns the provider type identifier.
func (p *EmailProvider) Type() string { return "email" }

// Execute sends an email for an action block.
func (p *EmailProvider) Execute(params ActionParams) error {
	block := params.Block
	if !p.cfg.Providers.Email.Enabled {
		return fmt.Errorf("email provider is not enabled")
	}

	if len(block.To) == 0 {
		return fmt.Errorf("no recipients specified")
	}

	subject, err := resolveTemplate(block.Subject, params.TagReader)
	if err != nil {
		return fmt.Errorf("resolve subject template: %w", err)
	}

	body, err := resolveTemplate(block.Body, params.TagReader)
	if err != nil {
		return fmt.Errorf("resolve body template: %w", err)
	}

	return p.SendEmail(block.To, subject, body)
}

// SendEmail sends an email to the given recipients.
func (p *EmailProvider) SendEmail(to []string, subject, body string) error {
	emailCfg := p.cfg.Providers.Email
	addr := net.JoinHostPort(emailCfg.Host, fmt.Sprintf("%d", emailCfg.Port))

	// Build the message
	from := emailCfg.From
	msg := buildEmailMessage(from, to, subject, body)

	// Connect
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}

	client, err := smtp.NewClient(conn, emailCfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	// STARTTLS if configured
	if emailCfg.UseTLS {
		tlsConfig := &tls.Config{ServerName: emailCfg.Host}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	// Authenticate if credentials are provided
	if emailCfg.Username != "" {
		auth := smtp.PlainAuth("", emailCfg.Username, emailCfg.Password, emailCfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	// Set sender
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("smtp MAIL: %w", err)
	}

	// Set recipients
	for _, addr := range to {
		if err := client.Rcpt(addr); err != nil {
			return fmt.Errorf("smtp RCPT %s: %w", addr, err)
		}
	}

	// Send body
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		w.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}

	client.Quit()
	p.logFn("email: sent to %v, subject=%q", to, subject)
	return nil
}

// buildEmailMessage constructs an RFC 2822 email message.
func buildEmailMessage(from string, to []string, subject, body string) string {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.String()
}

// resolveTemplate parses and executes a text/template with tag references.
// Templates use {{.Tag "plcName" "tagName"}} to reference PLC tag values.
func resolveTemplate(tmplStr string, tagReader TagReader) (string, error) {
	if tmplStr == "" {
		return "", nil
	}

	if tagReader == nil {
		return tmplStr, nil
	}

	funcMap := template.FuncMap{
		"Tag": func(plc, tag string) string {
			val, err := tagReader.ReadTagValue(plc, tag)
			if err != nil {
				return fmt.Sprintf("<err:%s.%s>", plc, tag)
			}
			return fmt.Sprintf("%v", val)
		},
	}

	tmpl, err := template.New("tmpl").Funcs(funcMap).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}
