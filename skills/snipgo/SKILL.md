---
name: snipgo
description: Use SnipGo on macOS for offline screenshot OCR, image redaction, subject cutouts, screenshot templates, pixel comparisons and MP4-to-GIF conversion.
---

Run `snipgo status --json` and check `data.tier1_ready`. The user can install the CLI in SnipGo settings under 命令行与 Agent. Pass explicit local input and output paths. Read the JSON object from stdout and diagnostics from stderr.

- Read text with `snipgo ocr input.png --format markdown --json`. Read table_markdown or table_tsv when has_table is true.
- Before sharing an image, run `snipgo redact input.png --out safe.png --json`. Review the output image before sharing. OCR-based redaction can miss sensitive content.
- Isolate a subject with `snipgo cutout input.png --trim-pad 0 --out subject.png --json`.
- List template keys and theme names with `snipgo template --list --json`, then run `snipgo template input.png --kind browser --theme 0 --out card.png --json`.
- Compare screenshots with `snipgo diff before.png after.png --out diff.png --max-diff-percent 0.1 --json`.
- Convert a local movie with `snipgo gif clip.mp4 --width 960 --fps 15 --out clip.gif --json`.

Exit 2 means correct the input. On permission or desktop access refusal (3), explain the setting to the user and stop. Exit 4 requires a running host, 5 means the target disappeared, 6 indicates processing or a diff gate failed, and 130 means canceled.

A null screen_capture_authorized means the host has not reported its permission state. File commands work independently of that permission. Do not prompt for a permission reset based on a null value.

For desktop requests, check tier2_ready. Ask the user to enable desktop and history access in SnipGo preferences if requested. Work with a window chosen by the user. Obtain its ID from `snipgo windows --app NAME --json`.

- `snipgo history --latest --json` returns the latest screenshot entry. Use `--redact-out DIR` to make a separate redacted copy.
- `snipgo capture --window ID --out capture.png --json` captures that window. Use `--rect x,y,w,h` or `--display ID` for a different explicitly requested target. Combine `--redact` and `--ocr` to recognize the redacted output.
- `snipgo record --window ID --duration 3 --out demo.gif --json` makes a silent clip. The duration is required and limited to 60 seconds. Disconnecting cancels the recording. Microphone input is unavailable. Region recording accepts explicit system audio. With a window target, --system-audio requests audio from its owning application, potentially including its other windows and tabs. The helper requires an identified owner and a window-specific filter. Before starting capture the helper checks the window filter against the selected window and its owner. When scope cannot be verified (including macOS before 15.2), the command returns an error without falling back to system-wide sound. Use MP4 for audio. GIF has no audio track. See `snipgo help record`.
- Use `snipgo pin image.png --json`, `snipgo pin --text "Review complete" --json`, or `snipgo pin --color "#123456" --json` to show a pin.
