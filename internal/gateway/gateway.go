package gateway

import (
	"context"
	"errors"
	"log"

	"github.com/mrchypark/relaker/internal/rules"
	"github.com/mrchypark/relaker/internal/runner"
)

type DedupeStore interface {
	CheckAndMark(keys []string) (bool, string)
	Unmark(keys []string)
}

type Gateway struct {
	rules  *rules.Set
	dedupe DedupeStore
	runner *runner.Runner
	logger *log.Logger
	ctx    context.Context
}

func New(ruleSet *rules.Set, dedupeStore DedupeStore, scriptRunner *runner.Runner) *Gateway {
	return NewWithContext(context.Background(), ruleSet, dedupeStore, scriptRunner)
}

func NewWithContext(ctx context.Context, ruleSet *rules.Set, dedupeStore DedupeStore, scriptRunner *runner.Runner) *Gateway {
	return &Gateway{
		rules:  ruleSet,
		dedupe: dedupeStore,
		runner: scriptRunner,
		logger: log.Default(),
		ctx:    ctx,
	}
}

func (g *Gateway) Handle(event rules.Event) {
	if err := g.Process(g.ctx, event, nil); err != nil {
		g.logger.Printf("stage=process result=error source=%s event=%s id=%s error=%q", event.Source, event.Event, event.ID, err)
	}
}

func (g *Gateway) Process(ctx context.Context, event rules.Event, extraEnv []string) error {
	g.logger.Printf("stage=receive source=%s event=%s action=%s id=%s envelope_id=%s", event.Source, event.Event, event.Action, event.ID, event.EnvelopeID)
	if duplicate, key := g.dedupe.CheckAndMark(event.DedupeKeys()); duplicate {
		g.logger.Printf("stage=dedupe result=skip source=%s event=%s key=%s reason=duplicate", event.Source, event.Event, key)
		return nil
	}

	matches, skips := g.rules.Match(event)
	for _, skip := range skips {
		g.logger.Printf("stage=filter result=skip source=%s event=%s run=%s reason=%q", event.Source, event.Event, skip.Rule.Run, skip.Reason)
	}
	ranAny := false
	for _, match := range matches {
		g.logger.Printf("stage=filter result=match source=%s event=%s run=%s", event.Source, event.Event, match.Run)
		if err := g.runner.Run(ctx, match, event, extraEnv); err != nil {
			if !ranAny {
				g.dedupe.Unmark(event.DedupeKeys())
			}
			g.logger.Printf("stage=execute result=error source=%s event=%s run=%s error=%q", event.Source, event.Event, match.Run, safeError(err))
			return err
		}
		ranAny = true
		g.logger.Printf("stage=execute result=ok source=%s event=%s run=%s", event.Source, event.Event, match.Run)
	}
	return nil
}

type safeErrorer interface {
	SafeError() string
}

func safeError(err error) string {
	var safe safeErrorer
	if errors.As(err, &safe) {
		return safe.SafeError()
	}
	return err.Error()
}
