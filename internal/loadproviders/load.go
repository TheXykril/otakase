// Package loadproviders exists to be imported for its side effect: each
// provider below registers itself from its init.
package loadproviders

import (
	_ "github.com/thexykril/otakase/internal/providers/anikoto"
	_ "github.com/thexykril/otakase/internal/providers/anizone"
	_ "github.com/thexykril/otakase/internal/providers/kickassanime"
	_ "github.com/thexykril/otakase/internal/providers/megavid"
	_ "github.com/thexykril/otakase/internal/providers/nyaa"
)
