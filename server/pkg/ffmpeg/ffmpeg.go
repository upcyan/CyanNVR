package ffmpeg

import (
	"bytes"
	"os/exec"
	"sync"
)

var lock sync.Mutex
var lastLog bytes.Buffer

func Exists(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

type Proc struct {
	cmd    *exec.Cmd
	stdout bytes.Buffer
	stderr bytes.Buffer
	done   chan error
	killed bool
	mu     sync.Mutex
}

func Start(bin string, args ...string) (*Proc, error) {
	cmd := exec.Command(bin, args...)
	p := &Proc{cmd: cmd, done: make(chan error, 1)}
	cmd.Stdout = &p.stdout
	cmd.Stderr = &p.stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() { p.done <- cmd.Wait() }()
	return p, nil
}

func (p *Proc) Wait() error { return <-p.done }

func (p *Proc) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.killed && p.cmd.ProcessState == nil
}

func (p *Proc) Kill() {
	p.mu.Lock()
	p.killed = true
	p.mu.Unlock()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

func (p *Proc) Log() string {
	return p.stderr.String() + p.stdout.String()
}

func (p *Proc) Pid() int {
	if p.cmd.Process != nil {
		return p.cmd.Process.Pid
	}
	return 0
}

func TestRTSP(bin, url string) bool {
	cmd := exec.Command(bin, "-rtsp_transport", "tcp", "-i", url, "-t", "3", "-f", "null", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return err == nil
}
