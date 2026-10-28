package server

import (
	"bufio"
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/eeyung1/meuseAI/db"
	"github.com/eeyung1/meuseAI/services"
)

func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
}

func ConfigFromEnv() (Config, error) {
	config := Config{Address: os.Getenv("MEUSEAI_ADDR"), PublicURL: os.Getenv("MEUSEAI_PUBLIC_URL"), SessionKey: []byte(os.Getenv("MEUSEAI_SESSION_KEY")), AdminToken: os.Getenv("MEUSEAI_ADMIN_TOKEN"), PaystackSecret: os.Getenv("PAYSTACK_SECRET_KEY"), LovableURL: os.Getenv("MEUSEAI_LOVABLE_URL"), OnboardingURL: os.Getenv("MEUSEAI_ONBOARDING_URL"), Logger: log.New(os.Stdout, "meuseai ", log.LstdFlags), Database: db.Config{Driver: os.Getenv("MEUSEAI_DB_DRIVER"), DSN: os.Getenv("MEUSEAI_DB_DSN")}}
	if config.Database.Driver == "" {
		config.Database.Driver = "sqlite"
	}
	if config.Database.Driver == "postgres" || config.Database.Driver == "postgresql" {
		config.Database.Schema = "meuseai"
	}
	if config.Database.Driver == "sqlite" && config.Database.DSN == "" {
		return config, errors.New("MEUSEAI_DB_DSN is required (use :memory: explicitly for disposable development)")
	}
	dry := false
	if raw := os.Getenv("MEUSEAI_DRY_RUN"); raw != "" {
		var err error
		dry, err = strconv.ParseBool(raw)
		if err != nil {
			return config, errors.New("invalid MEUSEAI_DRY_RUN")
		}
	}
	if dry {
		config.Mailer = &services.DryRunMailer{}
		config.Logger.Print("dry-run email enabled; delivery is captured in memory only")
	} else {
		mailer, err := NewResendMailer(os.Getenv("RESEND_API_KEY"), os.Getenv("MEUSEAI_EMAIL_FROM"), nil)
		if err != nil {
			return config, err
		}
		config.Mailer = mailer
	}
	return config, nil
}

func Main() error {
	loadEnvFile(".env")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	config, err := ConfigFromEnv()
	if err != nil {
		return err
	}
	app, err := New(ctx, config)
	if err != nil {
		return err
	}
	defer app.Close()
	return app.Serve(ctx)
}
