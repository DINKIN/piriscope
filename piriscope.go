package main

import (
  "os"
  "os/exec"
  "strings"
  "strconv"
  "encoding/json"
  "io/ioutil"
  "errors"
  "github.com/jawher/mow.cli"
  log "github.com/Sirupsen/logrus"
)

var version string
var commit string

type configuration struct {
  Periscope periscope `json:"periscope"`
  Video video `json:"video"`
}

type periscope struct {
  Key string `json:"key"`
}

type video struct {
  Width int `json:"width"`
  Height int `json:"height"`
  Sharpness int `json:"sharpness"`
  Quality int `json:"quality"`
  Bitrate int `json:"bitrate"`
  VFlip bool `json:"vflip"`
  HFlip bool `json:"hflip"`
}

func joinProps(props map[string]string, kvSeparator string, fieldSeparator string) string {
  parts := []string{}
  for key, value := range props {
    parts = append(parts, key + kvSeparator + value)
  }
  return strings.Join(parts, fieldSeparator)
}

func showCommand(cmd *exec.Cmd) {
  log.WithFields(log.Fields{
    "cmd": cmd.Path,
    "args": strings.Join(cmd.Args[1:], " "),
  }).Debug("Running command")
}

func mergeString(l string, r string) string {
  if r == "" {
    return l
  } else {
    return r
  }
}

func mergeInt(l int, r int) int {
  if r == 0 {
    return l
  } else {
    return r
  }
}

func mergeBool(l bool, r bool, def bool) bool {
  if r == def {
    return l
  } else {
    return r
  }
}

func mergePeriscope(l *periscope, r *periscope) *periscope {
  return &periscope{
    Key: mergeString(l.Key, r.Key),
  }
}

func mergeVideo(l *video, r *video) *video {
  return &video{
    Width: mergeInt(l.Width, r.Width),
    Height: mergeInt(l.Height, r.Height),
    Sharpness: mergeInt(l.Sharpness, r.Sharpness),
    Quality: mergeInt(l.Quality, r.Quality),
    Bitrate: mergeInt(l.Bitrate, r.Bitrate),
    VFlip: mergeBool(l.VFlip, r.VFlip, false),
    HFlip: mergeBool(l.HFlip, r.HFlip, false),
  }
}

func mergeConfig(l *configuration, r *configuration) *configuration {
  return &configuration{
    Periscope: *mergePeriscope(&l.Periscope, &r.Periscope),
    Video: *mergeVideo(&l.Video, &r.Video),
  }
}

func runStream(config *configuration) error {
  if config.Periscope.Key == "" {
    return errors.New("Periscope stream key (-k, --key) is required.")
  }

  streamUrl := "rtmp://va.pscp.tv:80/x/" + config.Periscope.Key

  width := strconv.Itoa(config.Video.Width)
  height := strconv.Itoa(config.Video.Height)
  sharpness := strconv.Itoa(config.Video.Sharpness)
  quality := strconv.Itoa(config.Video.Quality)
  bitrate := strconv.Itoa(config.Video.Bitrate)

  videoProps := map[string]string{
    "width": width,
    "height": height,
    "pixelformat": "4",
  }

  v4l2 := exec.Command("v4l2-ctl", "--set-fmt-video=" + joinProps(videoProps, "=", ","))
  v4l2.Stdout = os.Stdout
  v4l2.Stderr = os.Stderr

  showCommand(v4l2)
  if err := v4l2.Run(); err != nil {
    return err
  }

  controlProps := map[string]string{
    "sharpness": sharpness,
    "compression_quality": quality,
    "video_bitrate_mode": "1",
    "video_bitrate": bitrate,
    "vertical_flip": "0",
    "horizontal_flip": "0",
  }

  if config.Video.VFlip {
    controlProps["vertical_flip"] = "1"
  }

  if config.Video.HFlip {
    controlProps["horizontal_flip"] = "1"
  }

  v4l2 = exec.Command("v4l2-ctl", "--set-ctrl=" + joinProps(controlProps, "=", ","))
  v4l2.Stdout = os.Stdout
  v4l2.Stderr = os.Stderr

  showCommand(v4l2)
  if err := v4l2.Run(); err != nil {
    return err
  }

  ffmpegArgs := []string{
    "-re",                              // Read from input at its native framerate. Best for real-time/streaming output.
    "-f", "lavfi", "-i", "anullsrc",    // No input audio.
    "-f", "h264", "-i", "/dev/video0",  // Use H.264-formatted video from /dev/video0.
    "-acodec", "aac",                   // Use AAC codec for audio (Periscope requirement).
    "-b:a", "0",                        // Zero audio bitrate since we have no input audio.
    "-map", "0:a",                      // Use stream 0 for audio (anullsrc).
    "-map", "1:v",                      // Use stream 1 for video (stdin).
    "-f", "h264",                       // Use H.264 codec for video (Periscope requirement).
    "-vcodec", "copy",                  // Copy video data directly from input.
    "-g", "60",                         // Keyframe interval: one keyframe every 60 frames (2 seconds for 30 fps video; Periscope requirement).
    "-f", "flv",                        // Package output in a Flash Video container (Periscope requirement).
    streamUrl,                          // RTMP streaming destination.
  }

  ffmpeg := exec.Command("ffmpeg", ffmpegArgs...)
  ffmpeg.Stdout = os.Stdout
  ffmpeg.Stderr = os.Stderr

  defer func () {
    ffmpeg.Wait()
  }()

  showCommand(ffmpeg)
  if err := ffmpeg.Start(); err != nil {
    return err
  }

  return nil
}

func main() {
  app := cli.App("piriscope", "Piriscope - https://github.com/schmich/piriscope")
  key := app.StringOpt("k key", "", "Periscope stream key")
  conf := app.StringOpt("c conf", "", "Configuration file")
  verbose := app.BoolOpt("v verbose", false, "Verbose output")

  app.Version("version", "piriscope " + version + " " + commit)

  app.Action = func () {
    if *verbose {
      log.SetLevel(log.DebugLevel)
    }

    var fileConfig configuration
    if *conf != "" {
      content, err := ioutil.ReadFile(*conf)
      if err != nil {
        log.Fatal(err)
      }

      log.WithFields(log.Fields{ "file": *conf }).Info("Using configuration file")

      err = json.Unmarshal(content, &fileConfig)
      if err != nil {
        log.Fatal("Error in configuration file: ", err)
      }
    }

    defaultConfig := &configuration{
      Periscope: periscope{
        Key: "",
      },
      Video: video{
        Width: 960,
        Height: 540,
        Sharpness: 30,
        Quality: 80,
        Bitrate: 800000,
        VFlip: false,
        HFlip: false,
      },
    }

    cliConfig := &configuration{
      Periscope: periscope{
        Key: *key,
      },
    }

    config := mergeConfig(defaultConfig, mergeConfig(&fileConfig, cliConfig))
    if err := runStream(config); err != nil {
      log.Fatal(err)
    }
  }

  app.Run(os.Args)
}
