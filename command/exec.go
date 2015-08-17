package command

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/99designs/aws-vault/Godeps/_workspace/src/github.com/mitchellh/cli"
	"github.com/99designs/aws-vault/keyring"
	"github.com/99designs/aws-vault/vault"
)

const (
	DefaultSessionDuration = time.Hour * 10
)

type execSessionProvider interface {
	Session(conf vault.SessionConfig) (vault.SessionCredentials, error)
}

type execProfileConfig interface {
	Profile(name string) (*vault.Profile, error)
}

// Executes a subcommand with credentials passed to it via the environment
type ExecCommand struct {
	Ui              cli.Ui
	Keyring         keyring.Keyring
	env             []string
	sessionProvider execSessionProvider
	profileConfig   execProfileConfig
}

func (c *ExecCommand) Run(args []string) int {
	var (
		refresh, noSession, session bool
		profileName                 string
		sessionDuration             time.Duration
	)
	flagSet := flag.NewFlagSet("exec", flag.ExitOnError)
	flagSet.StringVar(&profileName, "profile", ProfileFromEnv(), "")
	flagSet.StringVar(&profileName, "p", ProfileFromEnv(), "")
	flagSet.BoolVar(&session, "session", true, "")
	flagSet.DurationVar(&sessionDuration, "duration", DefaultSessionDuration, "")
	flagSet.BoolVar(&refresh, "refresh", false, "")
	flagSet.BoolVar(&noSession, "no-session", false, "")
	flagSet.Usage = func() { c.Ui.Output(c.Help()) }

	if err := flagSet.Parse(args); err != nil {
		c.Ui.Error("Error parsing flags: " + err.Error())
		return 1
	}

	cmdArgs := flagSet.Args()
	if len(cmdArgs) < 1 {
		c.Ui.Output("Expected arguments: " + c.Help())
		return 1
	}

	if c.Keyring == nil {
		c.Keyring = keyring.DefaultKeyring
	}

	if c.profileConfig == nil {
		c.profileConfig = vault.DefaultProfileConfig
	}

	profile, err := c.profileConfig.Profile(profileName)
	if err != nil {
		c.Ui.Output(err.Error())
		return 1
	}

	if c.env == nil {
		c.env = os.Environ()
	}

	c.env = append(c.env, "AWS_DEFAULT_PROFILE="+profile.Name)

	if session && !noSession {
		if c.sessionProvider == nil {
			c.sessionProvider = &vault.KeyringSessionProvider{
				Keyring: c.Keyring,
				CredsFunc: func() (vault.Credentials, error) {
					return profile.Keyring(c.Keyring).Read()
				},
			}
		}
		sessCreds, err := c.sessionProvider.Session(vault.SessionConfig{
			Profile:    profile,
			TokenAgent: c,
			Duration:   sessionDuration,
		})
		if err != nil {
			c.Ui.Error(err.Error())
			return 1
		}
		c.env = append(c.env, sessCreds.Environ()...)
	} else {
		creds, err := profile.Keyring(c.Keyring).Read()
		if err != nil {
			c.Ui.Error(err.Error())
			return 1
		}
		c.env = append(c.env, creds.Environ()...)
	}

	bin, err := exec.LookPath(cmdArgs[0])
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	if err = syscall.Exec(bin, cmdArgs, c.env); err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	return 0
}

func (c *ExecCommand) GetToken(serial string) (string, error) {
	token, err := c.Ui.AskSecret(fmt.Sprintf("Enter token code for %q:", serial))
	if err != nil {
		return "", err
	}
	c.Ui.Output("")
	return token, nil
}

func (c *ExecCommand) Help() string {
	helpText := `
Usage: aws-vault exec [options] [cmd args...]
  Executes a command with the credentials from the given profile

Options:
  --profile=default         Which aws profile to use, defaults to $AWS_DEFAULT_PROFILE
  --[no-]session            Whether to generate an STS session [default: session]
  --duration=1h             The duration for the STS session generated
  --refresh                 Establish a new session, or refresh the existing one
`
	return strings.TrimSpace(helpText)
}

func (c *ExecCommand) Synopsis() string {
	return "Executes a command with the credentials from the given profile"
}
