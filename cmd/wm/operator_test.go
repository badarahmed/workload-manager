package main

import (
	"bufio"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestInteractiveInterruptHelper(t *testing.T) {
	if os.Getenv("WM_TEST_INTERRUPT_HELPER") != "1" {
		return
	}
	os.Exit(waitInteractive(exec.Command("/bin/sh", "-c", "trap 'exit 130' INT; echo READY; while :; do sleep 1; done")))
}
func TestOperatorForwardsInterruptAndReturnsChildExit(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestInteractiveInterruptHelper")
	cmd.Env = append(os.Environ(), "WM_TEST_INTERRUPT_HELPER=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if scanner.Text() == "READY" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("helper not ready")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper startup timeout")
	}
	if err = cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	select {
	case err := <-finished:
		if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 130 {
			t.Fatalf("exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("interrupt not forwarded")
	}
}
