# nissan-nav-tools

Tools for the map card in a Nissan Leaf's Clarion QY8 head unit.

- Back up: copies a card to an image, trimming the unused tail.
- Patch: skips the telematics consent screen, in place on the file or a card if you ask.
- Anonymize: clears phonebook, pairings, addresses, history and settings.
- Trim: cuts the dead space after the last partition.

Output is always a copy unless you pass `-in-place`.

## CLI

    nissan-nav-tools anonymize -in card.img [-out card.anon.img] [-force] [-in-place]
    nissan-nav-tools consent   -in card.img [-out card.consent.img] [-force] [-in-place]
    nissan-nav-tools trim      -in card.img [-out card.trim.img]   [-force] [-in-place]

No arguments (or `gui`) opens the window.

## Building

    go build -o nissan-nav-tools ./cmd/nissan-nav-tools

The GUI uses Fyne and cgo: build on the target OS, or use [fyne-cross](https://github.com/fyne-io/fyne-cross).

Prebuilt: see Releases.

Not affiliated with Nissan or Clarion.
