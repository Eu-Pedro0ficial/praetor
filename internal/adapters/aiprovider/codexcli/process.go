package codexcli

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

const maximumProcessOutputBytes = 2 << 20

type processInvocation struct {
	binary        string
	arguments     []string
	directory     string
	standardInput string
}

type processResult struct {
	standardOutput []byte
	standardError  []byte
	exitCode       int
	outputExceeded bool
}

type processRunner interface {
	Run(context.Context, processInvocation) (processResult, error)
}

type operatingSystemProcessRunner struct{}

func (operatingSystemProcessRunner) Run(
	ctx context.Context,
	invocation processInvocation,
) (processResult, error) {
	command := exec.CommandContext(ctx, invocation.binary, invocation.arguments...)
	command.Dir = invocation.directory
	command.Stdin = bytes.NewBufferString(invocation.standardInput)
	command.WaitDelay = 5 * time.Second

	standardOutput := newBoundedBuffer(maximumProcessOutputBytes)
	standardError := newBoundedBuffer(maximumProcessOutputBytes)
	command.Stdout = standardOutput
	command.Stderr = standardError

	runError := command.Run()
	result := processResult{
		standardOutput: standardOutput.Bytes(),
		standardError:  standardError.Bytes(),
		exitCode:       0,
		outputExceeded: standardOutput.Exceeded() || standardError.Exceeded(),
	}
	if runError == nil {
		return result, nil
	}
	result.exitCode = -1
	var exitError *exec.ExitError
	if errors.As(runError, &exitError) {
		result.exitCode = exitError.ExitCode()
	}
	return result, runError
}

// boundedBuffer retains a fixed prefix while continuing to accept writes so
// a verbose child cannot force unbounded memory use or block on a full pipe.
type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func newBoundedBuffer(limit int) *boundedBuffer {
	return &boundedBuffer{limit: limit}
}

func (buffer *boundedBuffer) Write(value []byte) (int, error) {
	originalLength := len(value)
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining <= 0 {
		buffer.exceeded = buffer.exceeded || originalLength > 0
		return originalLength, nil
	}
	if len(value) > remaining {
		buffer.exceeded = true
		value = value[:remaining]
	}
	_, _ = buffer.buffer.Write(value)
	return originalLength, nil
}

func (buffer *boundedBuffer) Bytes() []byte {
	return append([]byte(nil), buffer.buffer.Bytes()...)
}

func (buffer *boundedBuffer) Exceeded() bool { return buffer.exceeded }
