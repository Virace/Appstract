package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"appstract/internal/config"
	"appstract/internal/updater"
)

type commandOutput struct {
	level config.OutputLevel
	out   io.Writer
	err   io.Writer
	color bool

	mu             sync.Mutex
	progressActive bool
	inlineLen      int
	taskActive     bool
	taskTitle      string
	taskDots       int
	taskCancel     context.CancelFunc
}

func newCommandOutput(level config.OutputLevel, out io.Writer, err io.Writer) *commandOutput {
	return &commandOutput{
		level: level,
		out:   out,
		err:   err,
		color: shouldUseColor(out) && shouldUseColor(err),
	}
}

func (o *commandOutput) printDefault(format string, args ...any) {
	if o.level == config.OutputLevelSilent {
		return
	}
	msg := fmt.Sprintf(format, args...)
	o.writeLine(o.out, styleMessage(msg, o.color, styleDefault))
}

func (o *commandOutput) printDebug(format string, args ...any) {
	if o.level != config.OutputLevelDebug {
		return
	}
	msg := fmt.Sprintf(format, args...)
	o.writeLine(o.out, styleMessage("[dbg] "+msg, o.color, styleDebug))
}

func (o *commandOutput) printError(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	o.writeLine(o.err, styleMessage("[err] "+msg, o.color, styleError))
}

func (o *commandOutput) writeLine(w io.Writer, line string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stopTaskLocked(true)
	if o.progressActive {
		o.finishInlineLocked(true)
		o.progressActive = false
	}
	fmt.Fprintln(w, line)
}

func (o *commandOutput) onUpdaterMessage(level updater.MessageLevel, msg string) {
	if level == updater.MessageLevelDefault && strings.HasSuffix(msg, "...") {
		o.startTask(strings.TrimSuffix(msg, "..."))
		return
	}
	switch level {
	case updater.MessageLevelDebug:
		o.printDebug("%s", msg)
	default:
		o.printDefault("%s", msg)
	}
}

func (o *commandOutput) onUpdaterProgress(progress updater.DownloadProgress) {
	if o.level == config.OutputLevelSilent {
		return
	}

	line := renderDownloadLine(progress)
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stopTaskLocked(true)
	o.renderInlineLocked(line, styleProgress)
	if progress.Done {
		o.finishInlineLocked(true)
		o.progressActive = false
		return
	}
	o.progressActive = true
}

func renderDownloadLine(progress updater.DownloadProgress) string {
	app := progress.AppName
	if strings.TrimSpace(app) == "" {
		app = "app"
	}

	if progress.Total > 0 {
		percent := int(float64(progress.Downloaded) / float64(progress.Total) * 100)
		if percent > 100 {
			percent = 100
		}
		bar := progressBar(percent, 24)
		return fmt.Sprintf("downloading %-20s %3d%% %s %s/%s", app, percent, bar, humanBytes(progress.Downloaded), humanBytes(progress.Total))
	}
	return fmt.Sprintf("downloading %-20s %s", app, humanBytes(progress.Downloaded))
}

func progressBar(percent, width int) string {
	if width <= 0 {
		return "[]"
	}
	filled := percent * width / 100
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("=", filled) + strings.Repeat(".", width-filled) + "]"
}

func humanBytes(v int64) string {
	if v < 1024 {
		return fmt.Sprintf("%d B", v)
	}
	value := float64(v)
	suffix := []string{"B", "KB", "MB", "GB", "TB"}
	idx := 0
	for value >= 1024 && idx < len(suffix)-1 {
		value /= 1024
		idx++
	}
	return fmt.Sprintf("%.1f %s", value, suffix[idx])
}

func parseOutputLevel(raw string) (config.OutputLevel, error) {
	if strings.TrimSpace(raw) == "" {
		return config.OutputLevelDefault, nil
	}
	level, ok := config.ParseOutputLevel(raw)
	if !ok {
		return "", fmt.Errorf("invalid output level %q (expected: silent|default|debug)", raw)
	}
	return level, nil
}

func resolveOutputLevel(root string, override string) (config.OutputLevel, error) {
	if strings.TrimSpace(override) != "" {
		return parseOutputLevel(override)
	}
	cfg, err := config.Load(root)
	if err != nil {
		return "", err
	}
	return cfg.OutputLevel, nil
}

func (o *commandOutput) startTask(title string) {
	if o.level == config.OutputLevelSilent {
		return
	}

	o.mu.Lock()
	o.stopTaskLocked(true)
	if o.progressActive {
		o.finishInlineLocked(true)
		o.progressActive = false
	}
	o.taskActive = true
	o.taskTitle = strings.TrimSpace(title)
	o.taskDots = 1
	o.renderTaskInlineLocked()
	ctx, cancel := context.WithCancel(context.Background())
	o.taskCancel = cancel
	o.mu.Unlock()

	go func() {
		ticker := time.NewTicker(240 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				o.mu.Lock()
				if !o.taskActive {
					o.mu.Unlock()
					return
				}
				o.taskDots++
				if o.taskDots > 3 {
					o.taskDots = 1
				}
				o.renderTaskInlineLocked()
				o.mu.Unlock()
			}
		}
	}()
}

func (o *commandOutput) stopTaskLocked(printNewline bool) {
	if o.taskCancel != nil {
		o.taskCancel()
		o.taskCancel = nil
	}
	if !o.taskActive {
		return
	}
	o.taskActive = false
	o.taskTitle = ""
	o.taskDots = 0
	o.finishInlineLocked(printNewline)
}

func (o *commandOutput) renderTaskInlineLocked() {
	frame := spinnerFrames[(o.taskDots-1)%len(spinnerFrames)]
	line := fmt.Sprintf("[%s] %s%s", frame, o.taskTitle, strings.Repeat(".", o.taskDots))
	o.renderInlineLocked(line, styleRunning)
}

func (o *commandOutput) renderInlineLocked(plain string, style styleType) {
	output := plain
	if o.inlineLen > len(plain) {
		output += strings.Repeat(" ", o.inlineLen-len(plain))
	}
	if o.color {
		output = colorize(output, style)
	}
	fmt.Fprintf(o.out, "\r%s", output)
	o.inlineLen = len(plain)
}

func (o *commandOutput) finishInlineLocked(printNewline bool) {
	if o.inlineLen == 0 {
		return
	}
	if printNewline {
		fmt.Fprintln(o.out)
	} else {
		fmt.Fprintf(o.out, "\r%s\r", strings.Repeat(" ", o.inlineLen))
	}
	o.inlineLen = 0
}

type styleType int

const (
	styleDefault styleType = iota
	styleSuccess
	styleError
	styleDebug
	styleRunning
	styleProgress
)

var spinnerFrames = []string{"|", "/", "-", `\`}

func styleMessage(msg string, useColor bool, fallback styleType) string {
	if !useColor {
		return msg
	}
	switch {
	case strings.HasPrefix(msg, "[ok]"):
		return colorize(msg, styleSuccess)
	case strings.HasPrefix(msg, "[err]"):
		return colorize(msg, styleError)
	case strings.HasPrefix(msg, "[dbg]"):
		return colorize(msg, styleDebug)
	default:
		return colorize(msg, fallback)
	}
}

func colorize(msg string, style styleType) string {
	code := "37" // white
	switch style {
	case styleSuccess:
		code = "32" // green
	case styleError:
		code = "31" // red
	case styleDebug:
		code = "36" // cyan
	case styleRunning:
		code = "33" // yellow
	case styleProgress:
		code = "34" // blue
	}
	return "\x1b[" + code + "m" + msg + "\x1b[0m"
}

func shouldUseColor(w io.Writer) bool {
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
