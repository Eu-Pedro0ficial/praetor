# Praetor UI assets extracted from the supplied reference screenshot

Files:
- praetor-sidebar-logo.svg: scalable vectorized pixel-mask of the complete right-side brand block.
- praetor-sidebar-logo-transparent-6x.png: transparent high-resolution PNG of the same block.
- praetor-shield.svg / praetor-shield-transparent-8x.png: shield-only asset.
- praetor-mark.svg / praetor-mark-transparent-10x.png: small top-left shield mark.
- praetor-exit.svg / praetor-exit-transparent-10x.png: small top-right exit/door icon.
- praetor-sidebar-reference-6x.png: nearest-neighbor enlargement of the exact screenshot crop for visual comparison.
- praetor-shield-braille.txt: terminal-safe Unicode approximation derived from the shield.

Important:
The current Praetor UI is rendered as ANSI/readline terminal text. PNG/SVG files are useful as canonical
branding/reference assets, but ordinary ANSI terminals cannot display them portably inside the character grid.
For the current renderer, the Braille/Unicode representation is the smallest dependency-free way to reproduce
the reference more closely. Rendering actual PNG/SVG would require a terminal graphics protocol (Kitty, Sixel,
iTerm2, etc.) and capability detection/fallbacks.
