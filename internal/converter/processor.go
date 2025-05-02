package converter

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	frameW = 80
	frameH = 60
)

var (
	jobs   = make(map[string][]string)
	jobsMu sync.RWMutex
)

func StartJob(r *http.Request) (string, error) {
	file, _, err := r.FormFile("video")
	if err != nil {
		return "", err
	}
	defer file.Close()

	tmpF, err := os.CreateTemp("", "vid-*.mp4")
	if err != nil {
		return "", err
	}
	defer tmpF.Close()
	if _, err := io.Copy(tmpF, file); err != nil {
		return "", err
	}

	jobID := fmt.Sprintf("%d", time.Now().UnixNano())
	go process(tmpF.Name(), jobID)
	return jobID, nil
}

func process(path, jobID string) {
	cmd := exec.Command("ffmpeg", "-i", path,
		"-vf", "scale=80:-1,format=gray",
		"-f", "rawvideo", "pipe:1")
	out, _ := cmd.StdoutPipe()
	cmd.Start()

	frameSize := frameW * frameH
	buf := make([]byte, frameSize)
	var frames []string

	for {
		if _, err := io.ReadFull(out, buf); err != nil {
			break
		}
		ascii := BytesToASCII(buf, frameW)
		frames = append(frames, ascii)
	}
	cmd.Wait()

	jobsMu.Lock()
	jobs[jobID] = frames
	jobsMu.Unlock()
}

func StreamJob(w http.ResponseWriter, ctx context.Context, jobID string) {
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)

	jobsMu.RLock()
	frames, ok := jobs[jobID]
	jobsMu.RUnlock()
	if !ok {
		http.Error(w, "job not found", 404)
		return
	}

	for _, f := range frames {
		select {
		case <-ctx.Done():
			return
		default:
			fmt.Fprintf(w, "data: %s\n\n", f)
			flusher.Flush()
			time.Sleep(50 * time.Millisecond)
		}
	}
}
