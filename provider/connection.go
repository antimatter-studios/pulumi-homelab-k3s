package homelabk3s

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	homelab "github.com/antimatter-studios/pulumi-homelab/provider"
	"github.com/antimatter-studios/pulumi-homelab/provider/jscompat"
	"github.com/antimatter-studios/pulumi-homelab/provider/remote"
	"github.com/antimatter-studios/pulumi-homelab/provider/shell"
	"github.com/antimatter-studios/pulumi-homelab/provider/stat"
	"github.com/antimatter-studios/pulumi-homelab/provider/transport"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

type Config struct {
	homelab.Config
	factory remote.Factory
	target  *target
}

func (c *Config) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[*Config], error) {
	checked, err := c.Config.Check(ctx, req)
	if err != nil {
		return infer.CheckResponse[*Config]{}, configError(err)
	}
	return infer.CheckResponse[*Config]{Inputs: &Config{Config: *checked.Inputs}, Failures: checked.Failures}, nil
}

func (c *Config) Diff(ctx context.Context, req infer.DiffRequest[*Config, *Config]) (p.DiffResponse, error) {
	return c.Config.Diff(ctx, infer.DiffRequest[*homelab.Config, *homelab.Config]{
		ID: req.ID, Inputs: &req.Inputs.Config, State: &req.State.Config,
	})
}

func configError(err error) error {
	return errors.New(strings.ReplaceAll(err.Error(), "homelab:", Name+":"))
}

func (c *Config) Configure(ctx context.Context) error {
	// The upstream config validates its settings and constructs its private, lazy runner.
	// The upstream runner is private, so bind the same public transport for this provider.
	factory := c.factory
	if factory == nil {
		if err := c.Config.Configure(ctx); err != nil {
			return configError(err)
		}
		factory = newRunner
	}
	cfg := c.RunnerConfig()
	r, err := factory(ctx, cfg)
	if err != nil {
		return err
	}
	c.target = &target{r, cfg}
	return nil
}

func newRunner(_ context.Context, cfg remote.Config) (remote.Runner, error) {
	// Upstream Configure already validates the explicit sources and trust policy.
	socket := cfg.AgentSocket
	if cfg.AgentSocketEnv != "" {
		socket = os.Getenv(cfg.AgentSocketEnv)
	}
	keys := cfg.HostKeys
	if cfg.HostKeysFile != "" {
		data, err := os.ReadFile(cfg.HostKeysFile)
		if err != nil {
			return nil, fmt.Errorf("%s:hostKeysFile: %w", Name, err)
		}
		keys = strings.Split(string(data), "\n")
	}
	tc := transport.Config{AgentSocket: socket, StrictHostKeyChecking: cfg.StrictHostKeyChecking}
	if cfg.Identity != "" {
		tc.Identity = []byte(cfg.Identity)
	}
	for _, hop := range append(cfg.Jumps, remote.Jump{Address: cfg.Address, Port: cfg.Port, User: cfg.User, HostKeys: keys}) {
		tc.Hops = append(tc.Hops, transport.Hop{Address: hop.Address, Port: hop.Port, User: hop.User})
		host := hop.Address
		if hop.Port != 22 {
			host = "[" + host + "]:" + strconv.Itoa(hop.Port)
		}
		for _, key := range hop.HostKeys {
			key = strings.TrimSpace(key)
			if key == "" || strings.HasPrefix(key, "#") {
				continue
			}
			if strings.HasPrefix(key, "ssh-") || strings.HasPrefix(key, "ecdsa-") {
				key = host + " " + key
			}
			tc.HostKeys = append(tc.HostKeys, key)
		}
	}
	return transport.New(tc)
}

type target struct {
	runner remote.Runner
	cfg    remote.Config
}

func targetFrom(ctx context.Context) (*target, error) {
	cfg := infer.GetConfig[*Config](ctx)
	if cfg == nil || cfg.target == nil {
		return nil, errors.New("the provider is not configured")
	}
	return cfg.target, nil
}

type commandResult struct {
	out, stderr string
	code        int
}

func (t *target) command(privileged bool, command string) (string, []byte) {
	if !privileged || t.cfg.Escalation == "none" {
		return command, nil
	}
	if t.cfg.Escalation == "sudo-password" {
		return "sudo -S -p '' sh -c " + shell.Quote(command), []byte(t.cfg.SudoPassword + "\n")
	}
	return "sudo -n sh -c " + shell.Quote(command), nil
}

func (t *target) ask(ctx context.Context, privileged bool, command string) (commandResult, error) {
	command, stdin := t.command(privileged, command)
	return t.run(ctx, command, stdin)
}

func (t *target) run(ctx context.Context, command string, stdin []byte) (commandResult, error) {
	out, stderr, code, err := t.runner.Run(ctx, command, stdin)
	return commandResult{string(out), string(stderr), code}, err
}

func (t *target) must(ctx context.Context, privileged bool, command string) (string, error) {
	command, stdin := t.command(privileged, command)
	result, err := t.run(ctx, command, stdin)
	if err != nil {
		return "", err
	}
	if result.code != 0 {
		detail := jscompat.Trim(result.stderr)
		if detail == "" {
			detail = jscompat.Trim(result.out)
		}
		where := fmt.Sprintf("%s@%s:%d", t.cfg.User, t.cfg.Address, t.cfg.Port)
		if len(t.cfg.Jumps) != 0 {
			jumps := make([]string, len(t.cfg.Jumps))
			for i, jump := range t.cfg.Jumps {
				jumps[i] = fmt.Sprintf("%s@%s:%d", jump.User, jump.Address, jump.Port)
			}
			where += " via " + strings.Join(jumps, ",")
		}
		return "", fmt.Errorf("`%s` failed on %s (exit %d): %s", command, where, result.code, detail)
	}
	return result.out, nil
}

type remoteFile struct {
	mode, content string
}

func (t *target) readFile(ctx context.Context, path string) (*remoteFile, error) {
	q := shell.Quote(path)
	result, err := t.ask(ctx, true, "test -f "+q+" || exit 9; stat -c '%a %U %G' "+q+" && cat "+q)
	if err != nil || result.code == 9 {
		return nil, err
	}
	if result.code != 0 {
		return nil, fmt.Errorf("could not read %s: %s", path, jscompat.Trim(result.stderr))
	}
	first, content, _ := strings.Cut(result.out, "\n")
	return &remoteFile{stat.ParseFileStat(first).Mode, content}, nil
}

func valueOr[T any](v *T, fallback T) T {
	if v != nil {
		return *v
	}
	return fallback
}

func ptr[T any](v T) *T { return &v }

func differs[T comparable](old, want *T) bool {
	return old != nil && (want == nil || *old != *want)
}
