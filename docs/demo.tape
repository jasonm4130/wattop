# VHS tape for the README hero GIF and screenshot: `make demo` (needs
# https://github.com/charmbracelet/vhs). Every number on screen comes from
# `wattop --demo`, a seeded synthetic machine, so no real session, path or
# spend is ever recorded and every re-record looks the same.

Output docs/assets/wattop.gif

Require bash

Set Shell "bash"
Set FontFamily "Menlo"
Set FontSize 14
Set Width 1400
Set Height 750
Set Padding 16
Set Theme "Catppuccin Mocha"
Set Framerate 12
Set TypingSpeed 0

# `stty oxtabs` stops Bubble Tea using hard tabs for cursor motion; VHS's
# xterm.js tab stops disagree with them and leave stray glyphs behind.
Hide
Type "stty oxtabs; clear; ./bin/wattop --demo"
Enter
Sleep 2.5s
Show

# Graphs arrive pre-filled (the demo reduces two minutes of history before
# the first frame); let a few live cycles scroll through.
Sleep 3s
Screenshot docs/assets/wattop.png
Sleep 1s

# Walk the subagent tree, open the session detail, come back.
Down
Sleep 600ms
Down
Sleep 600ms
Down
Sleep 800ms
Enter
Sleep 4s
Enter
Sleep 1s

# Meters-only layout, then back to graphs.
Type "g"
Sleep 3s
Type "g"
Sleep 1.5s

Hide
Type "q"
Sleep 300ms
