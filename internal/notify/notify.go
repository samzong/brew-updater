package notify

import (
	"os/exec"
)

type Notifier struct {
	method string
}

func New(method string) *Notifier {
	return &Notifier{method: method}
}

func (n *Notifier) Notify(title, message, execute string) error {
	if n.method != "terminal-notifier" {
		return nil
	}
	path, err := exec.LookPath("terminal-notifier")
	if err != nil {
		return err
	}
	args := []string{"-title", title, "-message", message}
	if execute != "" {
		args = append(args, "-execute", execute)
	}
	cmd := exec.Command(path, args...)
	return cmd.Run()
}
