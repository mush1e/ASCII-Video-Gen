package converter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	frameW = 240
	frameH = 150
)

var asciiRamp = []rune(" .`-_':,;^=+/\"|)\\<>)iv%xclrs{*}I?!][1taeo7zjLunT#JCwfy325Fp6mqSghVd4EgXPGZbYkOA&8U$@KHDBWNMR")

func init() {
	for i, j := 0, len(asciiRamp)-1; i < j; i, j = i+1, j-1 {
		asciiRamp[i], asciiRamp[j] = asciiRamp[j], asciiRamp[i]
	}
}

type FrameData struct {
	Content string `json:"content"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

type Job struct {
	FramesChan chan *FrameData
}

var (
	jobs   = make(map[string]*Job)
	jobsMu sync.RWMutex
)

// BytesToASCII converts raw pixel data to ASCII art
func BytesToASCII(buf []byte, width int) string {
	var b strings.Builder
	for i, pix := range buf {
		idx := len(asciiRamp) - 1 - int(pix)*len(asciiRamp)/256
		if idx < 0 {
			idx = 0
		} else if idx >= len(asciiRamp) {
			idx = len(asciiRamp) - 1
		}
		b.WriteRune(asciiRamp[idx])

		if (i+1)%width == 0 {
			b.WriteRune('\n')
		}
	}
	return b.String()
}

func StartJob(r *http.Request) (string, error) {
	file, header, err := r.FormFile("video")
	if err != nil {
		return "", fmt.Errorf("error getting uploaded file: %v", err)
	}
	defer file.Close()

	log.Printf("Processing file: %s, size: %d bytes", header.Filename, header.Size)

	tmpF, err := os.CreateTemp("", "vid-*.mp4")
	if err != nil {
		return "", fmt.Errorf("error creating temp file: %v", err)
	}

	tmpPath := tmpF.Name()
	log.Printf("Created temporary file: %s", tmpPath)

	if _, err := io.Copy(tmpF, file); err != nil {
		tmpF.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("error copying file: %v", err)
	}
	tmpF.Close()

	jobID := fmt.Sprintf("%d", time.Now().UnixNano())

	framesChan := make(chan *FrameData, 300)

	jobsMu.Lock()
	jobs[jobID] = &Job{FramesChan: framesChan}
	jobsMu.Unlock()

	go process(tmpPath, jobID, framesChan)

	return jobID, nil
}

func process(path, jobID string, framesChan chan<- *FrameData) {
	defer func() {
		close(framesChan)

		jobsMu.Lock()
		delete(jobs, jobID)
		jobsMu.Unlock()

		os.Remove(path)
		log.Printf("Job %s completed, temporary file removed", jobID)
	}()

	infoCmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,duration,bit_rate",
		"-of", "json",
		path)

	infoOutput, err := infoCmd.Output()
	if err != nil {
		log.Printf("Error getting video info: %v", err)
	} else {
		log.Printf("Video info: %s", string(infoOutput))
	}

	cmd := exec.Command("ffmpeg",
		"-i", path,
		"-vf", fmt.Sprintf(
			"fps=10,eq=contrast=1.5:brightness=0.0,"+
				"scale=%d:%d:flags=lanczos:force_original_aspect_ratio=decrease,"+
				"pad=%d:%d:-1:-1:color=black,"+
				"setsar=1:2,format=gray",
			frameW, frameH, frameW, frameH,
		),
		"-f", "rawvideo",
		"-r", "10",
		"pipe:1",
	)

	out, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("Error creating stdout pipe: %v", err)
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("Error creating stderr pipe: %v", err)
		return
	}

	go func() {
		stderrData, _ := io.ReadAll(stderr)
		if len(stderrData) > 0 {
			log.Printf("FFmpeg stderr: %s", string(stderrData))
		}
	}()

	if err := cmd.Start(); err != nil {
		log.Printf("Error starting ffmpeg: %v", err)
		return
	}

	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("FFmpeg process ended with error: %v", err)
		}
	}()

	frameSize := frameW * frameH
	buf := make([]byte, frameSize)
	frameCount := 0

	for {
		_, err := io.ReadFull(out, buf)
		if err != nil {
			if err == io.EOF {
				log.Printf("Processed %d frames for job %s", frameCount, jobID)
				break
			}
			log.Printf("Error reading frame: %v", err)
			break
		}

		frameCount++

		ascii := BytesToASCII(buf, frameW)
		frameData := &FrameData{
			Content: ascii,
			Width:   frameW,
			Height:  frameH,
		}

		select {
		case framesChan <- frameData:
		default:
			log.Println("Dropped frame due to slow client")
		}
	}
}

func StreamJob(w http.ResponseWriter, ctx context.Context, jobID string) {
	// Get the job from the map
	jobsMu.RLock()
	job, ok := jobs[jobID]
	jobsMu.RUnlock()

	if !ok {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	frameInterval := time.NewTicker(100 * time.Millisecond)
	defer frameInterval.Stop()

	for {
		select {
		case frame, ok := <-job.FramesChan:
			if !ok {
				fmt.Fprintf(w, "event: end\ndata: {\"status\":\"complete\"}\n\n")
				flusher.Flush()
				return
			}

			<-frameInterval.C

			data, _ := json.Marshal(frame.Content)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

		case <-ctx.Done():
			return
		}
	}
}
