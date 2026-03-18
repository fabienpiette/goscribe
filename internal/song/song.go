package song

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func ExtractVocals(audioPath string) (vocalsPath string, cleanup func(), err error) {
	if _, err := exec.LookPath("demucs"); err != nil {
		return "", nil, fmt.Errorf("demucs not found: install with 'pip install demucs' or 'pipx install demucs'")
	}

	tmpDir, err := os.MkdirTemp("", "goscribe-demucs-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}

	output, err := exec.Command("demucs", "--two-stems=vocals", "-n", "htdemucs", "-o", tmpDir, audioPath).CombinedOutput()
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("demucs failed: %w\nOutput: %s", err, string(output))
	}

	stem := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
	vp := filepath.Join(tmpDir, "htdemucs", stem, "vocals.wav")
	return vp, func() { os.RemoveAll(tmpDir) }, nil
}
