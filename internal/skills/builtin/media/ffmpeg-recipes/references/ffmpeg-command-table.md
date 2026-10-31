# ffmpeg command table

For `ffmpeg-recipes`. `-y` overwrites without asking; prefer writing to a NEW file.

```bash
# Inspect
ffprobe -v error -show_streams -show_format in.mp4

# Remux (no re-encode)
ffmpeg -i in.mkv -c copy out.mp4

# Fast cut (keyframe-snapped)
ffmpeg -ss 00:01:00 -t 00:00:30 -i in.mp4 -c copy clip.mp4

# Accurate cut (slow, decodes)
ffmpeg -i in.mp4 -ss 00:01:00 -t 00:00:30 -c:v libx264 -crf 23 -c:a aac clip.mp4

# Compress for sharing
ffmpeg -i in.mov -c:v libx264 -crf 23 -preset medium -c:a aac -movflags +faststart out.mp4

# Scale down
ffmpeg -i in.mp4 -vf scale=1280:-2 -c:a copy out.mp4

# Extract audio
ffmpeg -i in.mp4 -vn -c:a libmp3lame -q:a 2 out.mp3

# Thumbnail at 5s
ffmpeg -ss 5 -i in.mp4 -frames:v 1 thumb.png

# GIF preview (palette for quality)
ffmpeg -ss 5 -t 3 -i in.mp4 -vf 'fps=10,scale=480:-1:flags=lanczos,palettegen' pal.png
ffmpeg -ss 5 -t 3 -i in.mp4 -i pal.png -lavfi 'fps=10,scale=480:-1:flags=lanczos [x]; [x][1:v] paletteuse' out.gif

# Concat same-codec parts
printf "file 'a.mp4'\nfile 'b.mp4'\n" > list.txt
ffmpeg -f concat -safe 0 -i list.txt -c copy joined.mp4
```
