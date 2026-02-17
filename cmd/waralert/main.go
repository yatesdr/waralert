package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"waralert/action"
	"waralert/audit"
	"waralert/chain"
	"waralert/config"
	"waralert/plcman"
	"waralert/sms"
	"waralert/whatsapp"
	"waralert/www"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "waralert.yaml", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Print version and exit")
	portOverride := flag.Int("p", 0, "Override web server port")
	hostOverride := flag.String("host", "", "Override web server host")
	adminUser := flag.String("admin-user", "", "Create admin user and exit")
	adminPass := flag.String("admin-pass", "", "Password for admin user (requires --admin-user)")
	logPath := flag.String("log", "", "Path to log file (default: stderr)")
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	if *showVersion {
		fmt.Println("waralert", version)
		os.Exit(0)
	}

	// Set up log output
	if *logPath != "" {
		f, err := os.OpenFile(*logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("failed to open log file: %v", err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// Handle --admin-user/--admin-pass: create user, save, exit
	if *adminUser != "" {
		if *adminPass == "" {
			log.Fatal("--admin-pass is required with --admin-user")
		}
		hash, err := www.HashPassword(*adminPass)
		if err != nil {
			log.Fatalf("failed to hash password: %v", err)
		}
		cfg.Lock()
		existing := cfg.FindWebUser(*adminUser)
		if existing != nil {
			existing.PasswordHash = hash
			existing.Role = config.RoleAdmin
		} else {
			cfg.AddWebUser(config.WebUser{
				Username:     *adminUser,
				PasswordHash: hash,
				Role:         config.RoleAdmin,
			})
		}
		if err := cfg.UnlockAndSave(*configPath); err != nil {
			log.Fatalf("failed to save config: %v", err)
		}
		fmt.Printf("Admin user %q created/updated in %s\n", *adminUser, *configPath)
		os.Exit(0)
	}

	// Apply overrides
	if *portOverride > 0 {
		cfg.Web.Port = *portOverride
	}
	if *hostOverride != "" {
		cfg.Web.Host = *hostOverride
	}

	logFn := func(format string, args ...interface{}) {
		if *debug {
			log.Printf(format, args...)
		}
	}

	// Initialize audit logger
	auditLog, err := audit.NewLogger(cfg.AuditLog, 1000)
	if err != nil {
		log.Fatalf("failed to create audit logger: %v", err)
	}
	defer auditLog.Close()

	// Initialize PLC manager
	plcMgr := plcman.NewManager(cfg.PollRate)
	plcMgr.LoadFromSources(cfg.Sources)
	plcMgr.Start()
	defer plcMgr.Stop()

	// Initialize action registry
	actionReg := action.NewRegistry(cfg)
	actionReg.SetLogFunc(logFn)

	// Create audit-logging action executor wrapper
	executor := &auditActionExecutor{
		registry: actionReg,
		auditLog: auditLog,
		logFn:    logFn,
	}

	// Initialize chain manager
	chainMgr := chain.NewManager(plcMgr, executor, logFn)
	chainMgr.LoadFromConfig(cfg.Chains)
	chainMgr.Start()
	defer chainMgr.Stop()

	// Initialize SMS subscription manager
	smsSender := &smsSenderAdapter{registry: actionReg}
	smsMgr := sms.NewManager(cfg, *configPath, logFn)
	smsHandler := sms.NewHandler(smsMgr, smsSender, cfg, logFn, auditLog)

	// Initialize WhatsApp client (always, so pairing UI works even before enabling)
	waDBPath := cfg.Providers.WhatsApp.DBPath
	if waDBPath == "" {
		waDBPath = "whatsapp.db"
	}
	waClient, err := whatsapp.NewClient(waDBPath, logFn)
	var waMgr *whatsapp.Manager
	if err != nil {
		log.Printf("whatsapp: init failed: %v (continuing without WhatsApp)", err)
		waClient = nil
	} else {
		actionReg.SetWhatsAppSender(waClient)
		waMgr = whatsapp.NewManager(cfg, *configPath, logFn)
		waHandler := whatsapp.NewHandler(waMgr, waClient, logFn, auditLog)
		waClient.SetMessageHandler(waHandler.HandleIncoming)
		if waClient.IsPaired() {
			if err := waClient.Connect(); err != nil {
				log.Printf("whatsapp: connect failed: %v", err)
			}
		} else {
			log.Println("whatsapp: not paired, use the web UI to pair")
		}
		defer waClient.Disconnect()
	}

	// Register config change listener for hot-reload
	cfg.AddOnChangeListener(func() {
		log.Println("config changed, reloading chains")
		chainMgr.LoadFromConfig(cfg.Chains)
		chainMgr.Start()
	})

	// Ensure TLS certificate exists (auto-generate if needed)
	configDir := filepath.Dir(*configPath)
	certFile := filepath.Join(configDir, "cert.pem")
	keyFile := filepath.Join(configDir, "key.pem")
	if err := ensureTLSCert(certFile, keyFile); err != nil {
		log.Fatalf("TLS setup failed: %v", err)
	}

	// Create cert reloader for hot-reload support
	certReloader, err := www.NewCertReloader(certFile, keyFile)
	if err != nil {
		log.Fatalf("failed to load TLS certificate: %v", err)
	}

	// Start auto-renewal goroutine
	stopAutoRenew := www.StartAutoRenew(certReloader, cfg, certFile, keyFile, auditLog)
	defer stopAutoRenew()

	// Create web server
	router, stopWeb := www.NewRouter(cfg, *configPath, certReloader, plcMgr, chainMgr, actionReg, smsMgr, smsHandler, waClient, waMgr, auditLog)
	defer stopWeb()

	addr := fmt.Sprintf("%s:%d", cfg.Web.Host, cfg.Web.Port)
	tlsCfg := &tls.Config{GetCertificate: certReloader.GetCertificate}
	server := &http.Server{
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start HTTPS server with hot-reloadable TLS
	go func() {
		ln, err := tls.Listen("tcp", addr, tlsCfg)
		if err != nil {
			log.Fatalf("TLS listen failed: %v", err)
		}
		log.Printf("waralert listening on https://%s", addr)
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("shutting down...")
	server.Close()
}

// auditActionExecutor wraps action.Registry to add audit logging.
type auditActionExecutor struct {
	registry *action.Registry
	auditLog *audit.Logger
	logFn    func(string, ...interface{})
}

func (e *auditActionExecutor) Execute(block config.BlockConfig, tagReader config.TagReader) error {
	err := e.registry.Execute(block, tagReader)

	entry := audit.Entry{
		EventType:  "action",
		Chain:      "", // filled by chain caller if needed
		Block:      block.Name,
		ActionType: block.ActionType,
		Success:    err == nil,
	}
	if block.Message != "" {
		entry.Message = block.Message
	}
	if block.Topic != "" {
		entry.Recipients = []string{"topic:" + block.Topic}
	} else if len(block.To) > 0 {
		entry.Recipients = block.To
	}
	if err != nil {
		entry.Error = err.Error()
	}
	e.auditLog.Log(entry)

	return err
}

// smsSenderAdapter adapts action.Registry to the sms.MessageSender interface.
type smsSenderAdapter struct {
	registry *action.Registry
}

func (a *smsSenderAdapter) SendMessage(phone, message string) error {
	return a.registry.TestSMS(phone, message)
}
