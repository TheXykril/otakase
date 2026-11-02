// Package rofitheme renders otakase's rofi menu themes from the active colour
// palette.
//
// The .rasi files used to be downloaded from GitHub on first run and then left
// alone forever, which meant otakase needed the network before it could show a menu,
// and any change to a theme never reached an existing install. They are embedded
// and rendered locally instead, so the menus work offline and follow the desktop
// theme.
package rofitheme

// GridLabelCapacity is how many monospace characters fit under one cover in the
// poster grid. Measured against the three-column layout in
// selectanimepreview.rasi by rendering a character ruler, not derived from the
// geometry: a first estimate from column arithmetic was off by a third.
const GridLabelCapacity = 37

// Names are the theme files otakase invokes rofi with.
var Names = []string{
	"selectanime.rasi",
	"selectanimepreview.rasi",
	"contextselect.rasi",
	"userinput.rasi",
}

// IconFile is the app icon's name beside the themes, which point at it.
const IconFile = "otakase-icon.png"

// BackupSuffix is appended to a hand-edited theme that had to be replaced.
const BackupSuffix = ".user-backup"
