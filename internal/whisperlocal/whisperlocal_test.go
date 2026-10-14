package whisperlocal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBinary writes a shell script that records its args and writes
// "<output_dir>/<audio base>.txt", like whisper-ctranslate2 does.
func fakeBinary(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-whisper")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("writing fake binary: %v", err)
	}
	orig := Binary
	Binary = path
	t.Cleanup(func() { Binary = orig })
	return dir
}

const writeOutput = `echo "$@" > "$ARGS_FILE"
audio="$1"
while [ $# -gt 0 ]; do
  if [ "$1" = "--output_dir" ]; then out="$2"; fi
  shift
done
base=$(basename "$audio"); base="${base%.*}"
printf '  hello from whisper  \n' > "$out/$base.txt"`

func TestTranscribe(t *testing.T) {
	dir := fakeBinary(t, writeOutput)
	argsFile := filepath.Join(dir, "args")
	t.Setenv("ARGS_FILE", argsFile)

	got, err := Transcribe(filepath.Join(dir, "meeting.reduce.mp3"), "", "fr")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello from whisper" {
		t.Errorf("transcript = %q, want %q", got, "hello from whisper")
	}

	args, _ := os.ReadFile(argsFile)
	for _, want := range []string{"--model small", "--language fr", "--output_format txt"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("args %q missing %q", args, want)
		}
	}
}

func TestTranscribeNoLanguageByDefault(t *testing.T) {
	dir := fakeBinary(t, writeOutput)
	argsFile := filepath.Join(dir, "args")
	t.Setenv("ARGS_FILE", argsFile)

	if _, err := Transcribe(filepath.Join(dir, "a.mp3"), "large-v3-turbo", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	args, _ := os.ReadFile(argsFile)
	if strings.Contains(string(args), "--language") {
		t.Errorf("args %q should not set --language", args)
	}
	if !strings.Contains(string(args), "--model large-v3-turbo") {
		t.Errorf("args %q should use the requested model", args)
	}
}

func TestTranscribeFailure(t *testing.T) {
	fakeBinary(t, "exit 3")

	_, err := Transcribe("a.mp3", "", "")
	if err == nil || !strings.Contains(err.Error(), "code 3") {
		t.Errorf("error = %v, want exit code 3", err)
	}
}

func TestTranscribeMissingBinary(t *testing.T) {
	orig := Binary
	Binary = "goscribe-no-such-binary"
	t.Cleanup(func() { Binary = orig })

	_, err := Transcribe("a.mp3", "", "")
	if err == nil || !strings.Contains(err.Error(), "uv tool install whisper-ctranslate2") {
		t.Errorf("error = %v, want install hint", err)
	}
}
