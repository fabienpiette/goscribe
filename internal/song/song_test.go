package song_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goscribe/internal/song"
)

func writeFakeDemucs(t *testing.T, exitCode int, stderr string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "demucs")
	body := `#!/bin/sh
outdir=""
input=""
for arg in "$@"; do
  case "$arg" in
    -o) shift; outdir="$1" ;;
    --*|-n|--two-stems=*) ;;
    *) input="$arg" ;;
  esac
  shift 2>/dev/null || true
done
# parse -o <dir> properly
args="$@"
`
	if exitCode != 0 {
		body = `#!/bin/sh
echo "` + stderr + `" >&2
exit ` + fmt.Sprintf("%d", exitCode)
	} else {
		body = `#!/bin/sh
outdir=""
input=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) outdir="$2"; shift 2 ;;
    -n|--two-stems=*) shift 2 ;;
    -*) shift ;;
    *) input="$1"; shift ;;
  esac
done
stem=$(basename "$input")
stem="${stem%.*}"
mkdir -p "$outdir/htdemucs/$stem"
touch "$outdir/htdemucs/$stem/vocals.wav"
`
	}
	os.WriteFile(script, []byte(body), 0755)
	return dir
}

func TestExtractVocals_Success(t *testing.T) {
	fakeDir := writeFakeDemucs(t, 0, "")
	t.Setenv("PATH", fakeDir+":"+os.Getenv("PATH"))

	audioDir := t.TempDir()
	audioPath := filepath.Join(audioDir, "mysong.mp3")
	os.WriteFile(audioPath, []byte("fake"), 0644)

	vocalsPath, cleanup, err := song.ExtractVocals(audioPath)
	if err != nil {
		t.Fatalf("ExtractVocals: %v", err)
	}
	defer cleanup()

	if !strings.HasSuffix(vocalsPath, "vocals.wav") {
		t.Errorf("vocalsPath %q does not end with vocals.wav", vocalsPath)
	}
	if _, err := os.Stat(vocalsPath); err != nil {
		t.Errorf("vocals.wav not found at %q: %v", vocalsPath, err)
	}

	cleanup()
	dir := filepath.Dir(filepath.Dir(filepath.Dir(vocalsPath)))
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("cleanup did not remove tmpdir %q", dir)
	}
}

func TestExtractVocals_DemucsNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, _, err := song.ExtractVocals("/some/song.mp3")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "demucs not found") {
		t.Errorf("error %q does not contain 'demucs not found'", err.Error())
	}
}

func TestExtractVocals_DemucsFailure(t *testing.T) {
	fakeDir := writeFakeDemucs(t, 1, "CUDA error: out of memory")
	t.Setenv("PATH", fakeDir+":"+os.Getenv("PATH"))

	audioDir := t.TempDir()
	audioPath := filepath.Join(audioDir, "song.mp3")
	os.WriteFile(audioPath, []byte("fake"), 0644)

	_, _, err := song.ExtractVocals(audioPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "demucs failed") {
		t.Errorf("error %q does not contain 'demucs failed'", err.Error())
	}
}
