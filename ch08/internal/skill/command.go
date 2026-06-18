package skill

import (
	"context"
	"fmt"

	"mewcode/internal/command"
)

type CommandExecutor interface {
	RunSkill(ctx context.Context, name, args string) error
}

func RegisterSkillCommands(reg *command.Registry, catalog *Catalog, executor CommandExecutor) error {
	if reg == nil || catalog == nil {
		return nil
	}
	for _, summary := range catalog.Summaries() {
		name := summary.Name
		def := command.Definition{
			Name:        "/" + name,
			Description: summary.Description + " [skill]",
			Usage:       "/" + name + " [args]",
			Kind:        command.KindPrompt,
			AcceptsArgs: true,
			SkillName:   name,
			Handler: func(skillName string) command.Handler {
				return func(ctx context.Context, inv command.Invocation, c command.Controller) (command.Result, error) {
					if executor != nil {
						if err := executor.RunSkill(ctx, skillName, inv.Args); err != nil {
							return command.Result{}, err
						}
						return command.Result{SentToAI: true}, nil
					}
					if runner, ok := c.(interface {
						RunSkill(context.Context, string, string) error
					}); ok {
						if err := runner.RunSkill(ctx, skillName, inv.Args); err != nil {
							return command.Result{}, err
						}
						return command.Result{SentToAI: true}, nil
					}
					return command.Result{}, fmt.Errorf("skill executor is not configured")
				}
			}(name),
		}
		if err := reg.Register(def); err != nil {
			return err
		}
	}
	return nil
}
