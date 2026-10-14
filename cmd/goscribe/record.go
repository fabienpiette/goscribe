package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"goscribe/internal/recorder"
	"goscribe/pkg/config"
)

const recordUsage = `goscribe record - record the mic and/or computer sound to an mp3

USAGE:
  goscribe record [flags]          (Ctrl+C to stop; a second Ctrl+C stops at once)

EXAMPLES:
  goscribe record                                   mic + computer sound
  goscribe record -source system -o webinar.mp3
  goscribe record -duration 45m -transcribe -provider codex -tags
  goscribe record -list-sources

Tip: use headphones. With speakers, the mic also picks up the computer sound,
so the other people are recorded twice and transcription gets worse.

FLAGS:
`

func recordingName(t time.Time) string {
	// The extension is appended: in a Go time layout the "3" of "mp3" means the hour.
	return t.Format("recording-2006-01-02_15-04-05") + ".mp3"
}

func pick(flagValue, configValue, fallback string) string {
	if flagValue != "" {
		return flagValue
	}
	if configValue != "" {
		return configValue
	}
	return fallback
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// runRecord implements "goscribe record" and returns the exit code:
// 0 recorded, 1 recording or transcription failed, 2 usage error.
func runRecord(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	fs.SetOutput(stderr)
	source := fs.String("source", "", "mic, system or both (default: config record_source, else both)")
	mic := fs.String("mic", "", "Input device (default: config record_mic, else the system default; see -list-sources)")
	system := fs.String("system", "", "Computer sound monitor (default: config record_system, else the default output's monitor)")
	output := fs.String("o", "", "Output mp3 (default: recording-YYYY-MM-DD_HH-MM-SS.mp3 in the current folder)")
	duration := fs.Duration("duration", 0, "Stop after this long, e.g. 90s, 45m, 1h30m (default: until Ctrl+C)")
	quality := fs.String("quality", "", "voice (mono 64 kbps) or music (stereo VBR) (default: config record_quality, else voice)")
	listSources := fs.Bool("list-sources", false, "List inputs and computer sound sources, then exit")
	configFile := fs.String("config", "", "Config file (default: ~/.goscribe/config.yml)")
	transcribe := fs.Bool("transcribe", false, "After stopping, transcribe the recording like 'goscribe <file.mp3>'")
	transcriber := fs.String("transcriber", "", "With -transcribe: transcription engine")
	provider := fs.String("provider", "", "With -transcribe: actions engine")
	action := fs.String("action", "", "With -transcribe: post-processing action ID(s)")
	auto := fs.Bool("auto", false, "With -transcribe: let the AI pick actions")
	tagsFlag := fs.Bool("tags", false, "With -transcribe: write a tags file")
	fs.Usage = func() {
		fmt.Fprint(stderr, recordUsage)
		fs.PrintDefaults()
	}

	rest, err := splitArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(rest) > 0 {
		fmt.Fprintf(stderr, "Error: unexpected argument %q (use -o for the output file)\n", rest[0])
		return 2
	}

	if *listSources {
		return printSources(stdout, stderr)
	}

	usageErr := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "Error: "+format+"\n", a...)
		return 2
	}
	defaults := config.ReadRecordDefaults(*configFile)
	src := pick(*source, defaults.Source, "both")
	qual := pick(*quality, defaults.Quality, "voice")
	if qual != "voice" && qual != "music" {
		return usageErr("invalid quality %q (use voice or music)", qual)
	}
	if isSet(fs, "duration") && *duration <= 0 {
		return usageErr("-duration must be positive (e.g. 45m)")
	}

	out := *output
	if out == "" {
		out = recordingName(time.Now())
	}
	if _, err := os.Stat(out); err == nil {
		return usageErr("%s already exists; pick another name with -o", out)
	}
	if info, err := os.Stat(filepath.Dir(out)); err != nil || !info.IsDir() {
		return usageErr("folder does not exist: %s", filepath.Dir(out))
	}

	inputs, err := recorder.Resolve(src, pick(*mic, defaults.Mic, ""), pick(*system, defaults.System, ""))
	if err != nil {
		return usageErr("%v", err)
	}

	// First Ctrl+C: stop cleanly. Second: kill at once.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	force := make(chan struct{})
	go func() {
		if _, ok := <-sigs; !ok {
			return
		}
		cancel()
		if _, ok := <-sigs; ok {
			close(force)
		}
	}()
	defer close(sigs)

	cfg := recorder.Config{Inputs: inputs, Quality: qual, Duration: *duration, Output: out}
	fmt.Fprintf(stdout, "Recording %s -> %s (Ctrl+C to stop)\n", cfg, out)
	res, err := recorder.Record(ctx, cfg, recorder.UI{Status: stdout, Interactive: isTerminal(stdout), Force: force})
	if err != nil {
		if errors.Is(err, recorder.ErrNothingRecorded) {
			fmt.Fprintln(stderr, "Nothing was recorded.")
		} else {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			if res.Size > 0 {
				fmt.Fprintf(stderr, "The partial recording is kept: %s\n", res.Output)
			}
		}
		return 1
	}
	fmt.Fprintf(stdout, "✓ Saved %s (%s, %s)\n", res.Output, recorder.FormatElapsed(res.Elapsed), recorder.FormatSize(res.Size))

	if *transcribe {
		err := run(runOptions{
			apiKey:         "XXXX",
			provider:       *provider,
			transcriber:    *transcriber,
			postAction:     *action,
			autoSelect:     *auto,
			tags:           *tagsFlag,
			configFile:     *configFile,
			enableFallback: true,
			args:           []string{res.Output},
		})
		if err != nil {
			fmt.Fprintf(stderr, "Error: transcription failed: %v\nThe recording is kept. Retry with: goscribe %s\n", err, res.Output)
			return 1
		}
	}
	return 0
}

func isSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func printSources(stdout, stderr io.Writer) int {
	sources, err := recorder.ListSources()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}
	defMic, _ := recorder.DefaultMic()
	defMon, _ := recorder.DefaultMonitor()
	section := func(title string, monitor bool, def string) {
		fmt.Fprintln(stdout, title)
		for _, s := range sources {
			if s.Monitor != monitor {
				continue
			}
			mark := " "
			if s.Name == def {
				mark = "*"
			}
			fmt.Fprintf(stdout, "  %s %s\n", mark, s.Name)
		}
	}
	section("Inputs (mic):", false, defMic)
	section("Computer sound (monitors):", true, defMon)
	fmt.Fprintln(stdout, "(* = default)")
	return 0
}
