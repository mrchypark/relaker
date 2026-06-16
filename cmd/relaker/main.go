package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mrchypark/relaker/internal/config"
	"github.com/mrchypark/relaker/internal/dedupe"
	"github.com/mrchypark/relaker/internal/gateway"
	githubrecv "github.com/mrchypark/relaker/internal/github"
	"github.com/mrchypark/relaker/internal/rules"
	"github.com/mrchypark/relaker/internal/runner"
	slackrecv "github.com/mrchypark/relaker/internal/slack"
)

func main() {
	var configPath string
	var addr string
	var root string
	var slackEnvelopePath string
	var slackWorkspace string
	flag.StringVar(&configPath, "config", "config/relaker.yaml", "YAML config path")
	flag.StringVar(&addr, "addr", "", "listen address override")
	flag.StringVar(&root, "root", ".", "root directory for allowlisted scripts")
	flag.StringVar(&slackEnvelopePath, "slack-envelope", "", "process one local Slack Socket Mode envelope JSON file and exit")
	flag.StringVar(&slackWorkspace, "slack-workspace", "", "workspace name for -slack-envelope")
	flag.Parse()

	if err := run(configPath, addr, root, slackEnvelopePath, slackWorkspace); err != nil {
		log.Fatal(err)
	}
}

func run(configPath, addrOverride, root, slackEnvelopePath, slackWorkspace string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if addrOverride != "" {
		cfg.Addr = addrOverride
	}
	if secret := os.Getenv("RELAKER_GITHUB_SECRET"); secret != "" {
		cfg.GitHubSecret = secret
	}

	ruleSet, err := rules.NewSet(cfg.Rules)
	if err != nil {
		return fmt.Errorf("build rules: %w", err)
	}
	scriptRunner, err := runner.New(root, ruleSet.Runs())
	if err != nil {
		return fmt.Errorf("build runner: %w", err)
	}
	gw := gateway.New(ruleSet, dedupe.NewMemoryStore(), scriptRunner)

	if slackEnvelopePath != "" {
		return processSlackEnvelope(context.Background(), slackEnvelopePath, slackWorkspace, gw)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	gw = gateway.NewWithContext(ctx, ruleSet, dedupe.NewMemoryStore(), scriptRunner)
	slackErr := startSlackIfConfigured(ctx, cfg.Slack.Workspaces, gw)

	mux := http.NewServeMux()
	if err := registerGitHubHandlers(mux, cfg, gw); err != nil {
		return err
	}
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("stage=start addr=%s", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	var runErr error
	select {
	case err := <-slackErr:
		runErr = err
	case err := <-serverErr:
		runErr = err
	case <-ctx.Done():
	}
	return finishRun(stop, server, gw, runErr)
}

type runWaiter interface {
	Wait()
}

func finishRun(stop context.CancelFunc, server *http.Server, waiter runWaiter, runErr error) error {
	stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil && runErr == nil {
		runErr = err
	}
	waiter.Wait()
	return runErr
}

func registerGitHubHandlers(mux *http.ServeMux, cfg *config.Config, gw *gateway.Gateway) error {
	if len(cfg.GitHub.Receivers) == 0 {
		if cfg.GitHubSecret == "" && !cfg.GitHubAllowUnsigned {
			return fmt.Errorf("github_secret is required unless github_allow_unsigned is true")
		}
		mux.Handle("/github", githubrecv.NewHandler(cfg.GitHubSecret, gw))
		log.Printf("stage=start source=github endpoint=/github")
		return nil
	}
	for _, receiver := range cfg.GitHub.Receivers {
		secret := receiver.Secret()
		if secret == "" && !receiver.AllowUnsigned {
			return fmt.Errorf("github receiver %s: secret_env %s is not set", receiver.Name, receiver.SecretEnv)
		}
		mux.Handle(receiver.Path, githubrecv.NewHandler(secret, receiverSink{name: receiver.Name, sink: gw}))
		log.Printf("stage=start source=github receiver=%s endpoint=%s", receiver.Name, receiver.Path)
	}
	return nil
}

func startSlackIfConfigured(ctx context.Context, workspaces []config.SlackWorkspace, gw *gateway.Gateway) <-chan error {
	if len(workspaces) > 0 {
		errCh := make(chan error, len(workspaces))
		for _, workspace := range workspaces {
			appToken, botToken, ok := workspace.Tokens()
			if !ok {
				log.Printf("stage=start source=slack workspace=%s result=disabled reason=missing_tokens", workspace.Name)
				continue
			}
			client := slackrecv.NewSocketModeClient(appToken, botToken)
			sink := workspaceSink{name: workspace.Name, sink: gw}
			go func(workspace string, client slackrecv.SocketClient, sink workspaceSink) {
				log.Printf("stage=start source=slack workspace=%s result=enabled mode=socket", workspace)
				if err := slackrecv.RunSocketMode(ctx, client, sink); err != nil {
					log.Printf("stage=socket source=slack workspace=%s result=error error=%q", workspace, err)
					errCh <- fmt.Errorf("slack workspace %s: %w", workspace, err)
				}
			}(workspace.Name, client, sink)
		}
		return errCh
	}
	errCh := make(chan error, 1)
	appToken := os.Getenv("SLACK_APP_TOKEN")
	botToken := os.Getenv("SLACK_BOT_TOKEN")
	if appToken == "" || botToken == "" {
		log.Printf("stage=start source=slack result=disabled reason=missing_tokens")
		return nil
	}
	client := slackrecv.NewSocketModeClient(appToken, botToken)
	go func() {
		log.Printf("stage=start source=slack result=enabled mode=socket")
		if err := slackrecv.RunSocketMode(ctx, client, gw); err != nil {
			log.Printf("stage=socket source=slack result=error error=%q", err)
			errCh <- fmt.Errorf("slack: %w", err)
		}
	}()
	return errCh
}

func processSlackEnvelope(ctx context.Context, path, workspace string, gw *gateway.Gateway) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read slack envelope: %w", err)
	}
	processor := slackrecv.NewProcessor()
	event, ok, err := processor.ProcessEnvelope(ctx, data, logAcker{})
	if err != nil {
		return err
	}
	if !ok {
		log.Printf("stage=slack result=skip reason=unsupported_payload")
		return nil
	}
	event.Workspace = workspace
	return gw.Process(ctx, event, nil)
}

type logAcker struct{}

func (logAcker) Ack(envelopeID string) error {
	log.Printf("stage=ack source=slack envelope_id=%s", envelopeID)
	return nil
}

type eventSink interface {
	Handle(rules.Event)
}

type asyncEventSink interface {
	HandleAsync(rules.Event, func())
}

type receiverSink struct {
	name string
	sink eventSink
}

func (s receiverSink) Handle(event rules.Event) {
	event.Receiver = s.name
	s.sink.Handle(event)
}

func (s receiverSink) HandleAsync(event rules.Event, done func()) {
	event.Receiver = s.name
	if sink, ok := s.sink.(asyncEventSink); ok {
		sink.HandleAsync(event, done)
		return
	}
	go func() {
		defer func() {
			if done != nil {
				done()
			}
			if recovered := recover(); recovered != nil {
				log.Printf("stage=dispatch result=panic source=github receiver=%s event=%s id=%s panic=%v", s.name, event.Event, event.ID, recovered)
			}
		}()
		s.sink.Handle(event)
	}()
}

type workspaceSink struct {
	name string
	sink eventSink
}

func (s workspaceSink) Handle(event rules.Event) {
	event.Workspace = s.name
	s.sink.Handle(event)
}

func (s workspaceSink) HandleAsync(event rules.Event, done func()) {
	event.Workspace = s.name
	if sink, ok := s.sink.(asyncEventSink); ok {
		sink.HandleAsync(event, done)
		return
	}
	go func() {
		defer func() {
			if done != nil {
				done()
			}
			if recovered := recover(); recovered != nil {
				log.Printf("stage=dispatch result=panic source=slack workspace=%s event=%s id=%s panic=%v", s.name, event.Event, event.ID, recovered)
			}
		}()
		s.sink.Handle(event)
	}()
}
