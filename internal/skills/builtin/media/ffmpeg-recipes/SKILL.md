---
name: ffmpeg-recipes
description: "Convert, cut and inspect audio and video with ffmpeg and ffprobe without re-encoding blindly."
metadata:
  excalibur:
    group: media
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: ffmpeg recipes
      name_es: Recetas ffmpeg
      desc_en: "Convert, cut and inspect audio and video with ffmpeg and ffprobe without re-encoding blindly."
      desc_es: "Convertir, cortar e inspeccionar audio y vídeo con ffmpeg."
---
# ffmpeg recipes

Use this skill for media conversion, trimming and inspection. Stream-copy (`-c copy`) whenever the codec already fits; re-encode only when the container or codec must change.

## When to use

- Converting between mp4/mkv/webm/mp3/wav.
- Cutting a segment or extracting audio, thumbnails or metadata.
- Diagnosing "plays here, fails there" codec problems.

## Procedure

1. Inspect first: `ffprobe -v error -show_streams -show_format in.mp4` (codecs, duration, resolution, sample rate).
2. Remux without re-encoding: `ffmpeg -i in.mkv -c copy out.mp4`; cut losslessly on keyframes: `ffmpeg -ss 00:01:00 -t 00:00:30 -i in.mp4 -c copy clip.mp4` (`-ss` before `-i` is fast seek).
3. Re-encode deliberately: `ffmpeg -i in.mov -c:v libx264 -crf 23 -preset medium -c:a aac out.mp4` (CRF 18–28 sane range; lower is better); scale with `-vf scale=1280:-2`.
4. Extract: audio `ffmpeg -i in.mp4 -vn -c:a libmp3lame -q:a 2 out.mp3`; thumbnails `ffmpeg -ss 5 -i in.mp4 -frames:v 1 thumb.png`.
5. Verify with `ffprobe` on the output and a 5-second playback check; keep the source until the output is confirmed.

## Pitfalls

- Re-encoding an already-compressed file degrades quality every generation; prefer `-c copy` chains.
- `-ss` after `-i` is frame-accurate but slow (decodes everything); before `-i` is fast but keyframe-snapped — pick per need.
- Overwriting inputs in place corrupts on failure; always write to a new file, then rename.

## Verification

- `ffprobe` shows the intended codecs/duration; the output plays; file size and quality spot-check pass.

## References

- `references/ffmpeg-command-table.md` — one-liners for convert, cut, extract and inspect.
