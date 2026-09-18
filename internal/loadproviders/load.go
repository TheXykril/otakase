// Package loadproviders registers built-in streaming providers via init side effects.
package loadproviders

import (
	_ "github.com/thexykril/otakase/internal/providers/anidb"
	_ "github.com/thexykril/otakase/internal/providers/anikoto"
	_ "github.com/thexykril/otakase/internal/providers/anineko"
	_ "github.com/thexykril/otakase/internal/providers/anipub"
	_ "github.com/thexykril/otakase/internal/providers/kickassanime"
	_ "github.com/thexykril/otakase/internal/providers/nyaa"
)
