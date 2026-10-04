--[[
otakase's controls for mpv, replacing mpv's own (--osc=no).

  * a top bar: a back button (quits mpv, back to otakase), the title, and
    chips naming the tracker and the source,
  * a bottom bar: the seek bar with the opening and ending marked on it,
    previous / play / next, time, volume, and on the right the episodes,
    audio and subtitle menus, the picture height and fullscreen,
  * a "Skip Opening" / "Skip Ending" button while one plays and otakase is
    not skipping it already, a "Skipped Opening · Undo" notice when it did,
  * an "Up next" card during the ending, while the playlist has a next episode,
  * a thin progress line when the controls are hidden.

Openings and endings are the "Opening" and "Ending" chapters otakase writes
into the chapter list. Colours arrive as script-opts at launch, the chips as
an `otakase-state` message from otakase. Everything is drawn at 720p and
scaled to the window.
]]

local mp = require('mp')
local utils = require('mp.utils')
local options = require('mp.options')

local opts = {
	icon_font = 'Material Icons Round',
	background = '000000',
	surface = '333333',
	foreground = 'E6E6FA',
	bright = 'FFFFFF',
	dim = '9A9A9A',
	accent = '7CB9E8',
	accent_text = '000000',
	highlight = 'FFD700',
	skip_op = false,
	skip_ed = false,
	hide_after = 2.0,
}
options.read_options(opts, 'otakase_skin')

-- otakase skips a span only when it sees playback within its first two seconds,
-- and checks once a second. Seeking back to here is past that window, so undo
-- is not skipped again.
local AUTO_SKIP_WINDOW = 2.5
local NOTICE_SECONDS = 5
local NEXT_UP_LEAD = 30

-- Colours ------------------------------------------------------------------

-- ASS writes colours as BBGGRR.
local function ass_color(hex)
	hex = (hex or ''):gsub('^#', '')
	if #hex ~= 6 then return 'FFFFFF' end
	return hex:sub(5, 6) .. hex:sub(3, 4) .. hex:sub(1, 2)
end

local function ass_alpha(opacity)
	opacity = math.max(0, math.min(1, opacity or 1))
	return string.format('%02X', math.floor((1 - opacity) * 255 + 0.5))
end

local C = {
	bg = ass_color(opts.background),
	surface = ass_color(opts.surface),
	fg = ass_color(opts.foreground),
	bright = ass_color(opts.bright),
	dim = ass_color(opts.dim),
	accent = ass_color(opts.accent),
	accent_text = ass_color(opts.accent_text),
	highlight = ass_color(opts.highlight),
}

-- State --------------------------------------------------------------------

local overlay = mp.create_osd_overlay('ass-events')
local state = {
	pos = nil,
	duration = nil,
	cache_end = nil,
	chapters = {},
	paused = false,
	fullscreen = false,
	volume = 100,
	muted = false,
	title = '',
	height = nil,
	tracks = {},
	playlist = {},
	playlist_pos = -1,
	mouse = {x = -1, y = -1, hover = false},
	shown_until = 0,
	notice = nil,              -- {label, until_time, undo_to}
	next_up_hidden_for = nil,  -- playlist position the card was dismissed on
	chips = {},
	menu = nil,                -- {kind, items, scroll, anchor_x, anchor_y}
	dragging = false,
}

local hits = {}        -- interactive rectangles drawn this frame
local hovered = nil    -- id of the hit under the pointer
local enter_action = nil
local bound = {}       -- names of the forced bindings currently held
local request_render

-- Drawing --------------------------------------------------------------------

local function rounded_rect(ax, ay, bx, by, r)
	r = math.max(0, math.min(r, (bx - ax) / 2, (by - ay) / 2))
	local k = r * 0.4477 -- (1 - 0.5523), bezier circle approximation
	return string.format(
		'm %.1f %.1f l %.1f %.1f b %.1f %.1f %.1f %.1f %.1f %.1f l %.1f %.1f b %.1f %.1f %.1f %.1f %.1f %.1f l %.1f %.1f b %.1f %.1f %.1f %.1f %.1f %.1f l %.1f %.1f b %.1f %.1f %.1f %.1f %.1f %.1f',
		ax + r, ay, bx - r, ay,
		bx - k, ay, bx, ay + k, bx, ay + r,
		bx, by - r,
		bx, by - k, bx - k, by, bx - r, by,
		ax + r, by,
		ax + k, by, ax, by - k, ax, by - r,
		ax, ay + r,
		ax, ay + k, ax + k, ay, ax + r, ay)
end

local function box(ax, ay, bx, by, r, color, opacity, border_color, border_opacity)
	local tags = string.format('{\\pos(0,0)\\an7\\bord0\\shad0\\blur0\\1c&H%s&\\1a&H%s&', color, ass_alpha(opacity))
	if border_color then
		tags = tags .. string.format('\\bord1\\3c&H%s&\\3a&H%s&', border_color, ass_alpha(border_opacity or 1))
	end
	return tags .. '\\p1}' .. rounded_rect(ax, ay, bx, by, r) .. '{\\p0}'
end

local function circle(x, y, r, color, opacity, border_color, border_width, border_opacity)
	local tags = string.format('{\\pos(0,0)\\an7\\shad0\\blur0\\1c&H%s&\\1a&H%s&', color, ass_alpha(opacity))
	if border_color then
		tags = tags .. string.format('\\bord%.1f\\3c&H%s&\\3a&H%s&', border_width, border_color, ass_alpha(border_opacity))
	else
		tags = tags .. '\\bord0'
	end
	return tags .. '\\p1}' .. rounded_rect(x - r, y - r, x + r, y + r, r) .. '{\\p0}'
end

-- A vertical fade from opacity `from` at y0 to `to` at y1. ASS has no
-- gradients, so it is drawn as bands a few pixels tall that meet exactly:
-- overlapping bands would double their alpha into visible lines.
local function fade(x0, y0, x1, y1, color, from, to)
	local bands = math.max(1, math.floor((y1 - y0) / 3))
	local out = {}
	local step = (y1 - y0) / bands
	for i = 0, bands - 1 do
		local t = (i + 0.5) / bands
		local opacity = from + (to - from) * t
		local ay = math.floor(y0 + step * i + 0.5)
		local by = math.floor(y0 + step * (i + 1) + 0.5)
		if by > ay and opacity > 0.004 then
			out[#out + 1] = string.format('{\\pos(0,0)\\an7\\bord0\\shad0\\blur0\\1c&H%s&\\1a&H%s&\\p1}m %d %d l %d %d l %d %d l %d %d{\\p0}',
				color, ass_alpha(opacity), x0, ay, x1, ay, x1, by, x0, by)
		end
	end
	return table.concat(out, '\n')
end

-- Titles come from providers; a brace in one would open an ASS tag. ASS has no
-- escape for a backslash, so one is followed by a zero-width space instead.
local function escape(value)
	value = tostring(value):gsub('\\', '\\\239\187\191')
	value = value:gsub('{', '\\{'):gsub('}', '\\}'):gsub('\n', ' ')
	return value
end

local function text_font()
	return mp.get_property('options/osd-font') or 'sans-serif'
end

local function text(x, y, align, value, size, color, opacity, bold, font)
	return string.format('{\\pos(%.1f,%.1f)\\an%d\\bord0\\shad0\\blur0\\fn%s\\fs%.1f\\b%d\\1c&H%s&\\1a&H%s&}%s',
		x, y, align, font or text_font(), size, bold and 1 or 0, color, ass_alpha(opacity), escape(value))
end

local function icon(x, y, name, size, color, opacity)
	return string.format('{\\pos(%.1f,%.1f)\\an5\\bord0\\shad0\\blur0\\fn%s\\fs%.1f\\b0\\1c&H%s&\\1a&H%s&}%s',
		x, y, opts.icon_font, size, color, ass_alpha(opacity), name)
end

-- Widths come from libass itself: a hidden overlay with compute_bounds
-- returns the box its text was drawn in.
local measurer = mp.create_osd_overlay('ass-events')
measurer.hidden = true
measurer.compute_bounds = true
local widths = {}
local width_count = 0

local function set_measure_size(w, h)
	if measurer.res_x ~= w or measurer.res_y ~= h then
		measurer.res_x, measurer.res_y = w, h
		widths, width_count = {}, 0
	end
end

local function text_width(value, size, bold, font)
	value = tostring(value)
	if value == '' then return 0 end
	local key = string.format('%s\0%.2f\0%s\0%s', value, size, bold and 1 or 0, font or '')
	local width = widths[key]
	if width then return width end
	measurer.data = text(0, 0, 7, value, size, 'FFFFFF', 1, bold, font)
	local bounds = measurer:update()
	if bounds and bounds.x1 and bounds.x0 and bounds.x1 > bounds.x0 then
		width = bounds.x1 - bounds.x0
	else
		local chars = 0
		for _ in value:gmatch('[^\128-\191]') do chars = chars + 1 end
		width = chars * size * 0.6
	end
	if width_count > 1000 then widths, width_count = {}, 0 end
	widths[key], width_count = width, width_count + 1
	return width
end

local function clock(seconds)
	if not seconds or seconds < 0 then seconds = 0 end
	seconds = math.floor(seconds)
	local h, m, s = math.floor(seconds / 3600), math.floor(seconds / 60) % 60, seconds % 60
	if h > 0 then return string.format('%d:%02d:%02d', h, m, s) end
	return string.format('%d:%02d', m, s)
end

local function hit(id, ax, ay, bx, by, action, extra)
	local h = {id = id, ax = ax, ay = ay, bx = bx, by = by, action = action}
	if extra then for k, v in pairs(extra) do h[k] = v end end
	hits[#hits + 1] = h
	return h
end

-- Layout ---------------------------------------------------------------------

local function scale(h)
	return math.max(0.6, math.min(3, h / 720))
end

local function chapter_span(title)
	for i, chapter in ipairs(state.chapters) do
		if chapter.title == title then
			local next_chapter = state.chapters[i + 1]
			local stop = (next_chapter and next_chapter.time) or state.duration
			if stop and stop > chapter.time then return chapter.time, stop end
		end
	end
end

local function chapter_at(t)
	local name = nil
	for _, chapter in ipairs(state.chapters) do
		if chapter.time <= t then name = chapter.title else break end
	end
	return name
end

local function playlist_has_next()
	local pos = state.playlist_pos
	return pos >= 0 and pos + 1 < #state.playlist, pos
end

-- otakase names every episode in its playlist. Anything else is a file or a
-- link, which reads worse than no name at all.
local function entry_title(index)
	local entry = state.playlist[index + 1]
	local title = entry and entry.title or ''
	if title == '' then return nil end
	return title
end

local function shorten(value, max)
	if #value > max then return value:sub(1, max - 3) .. '...' end
	return value
end

local function current_track(kind)
	for _, track in ipairs(state.tracks) do
		if track.type == kind and track.selected then return track end
	end
end

local languages = {
	en = 'English', eng = 'English', ja = 'Japanese', jpn = 'Japanese', es = 'Spanish', spa = 'Spanish',
	pt = 'Portuguese', por = 'Portuguese', fr = 'French', fre = 'French', fra = 'French',
	de = 'German', ger = 'German', deu = 'German', it = 'Italian', ita = 'Italian', ru = 'Russian',
	rus = 'Russian', ar = 'Arabic', ara = 'Arabic', zh = 'Chinese', chi = 'Chinese', zho = 'Chinese',
	ko = 'Korean', kor = 'Korean', id = 'Indonesian', ind = 'Indonesian', th = 'Thai', tha = 'Thai',
	vi = 'Vietnamese', vie = 'Vietnamese', pl = 'Polish', pol = 'Polish', tr = 'Turkish', tur = 'Turkish',
	hi = 'Hindi', hin = 'Hindi',
}

local function track_label(track)
	if not track then return nil end
	local label = track.title
	local base, ext = (label or ''):lower():match('^(.-)%.(%a+)$')
	if ext == 'srt' or ext == 'ass' or ext == 'ssa' or ext == 'vtt' then
		-- An external file's title is its file name; en.srt still says English.
		label = languages[base:match('([%a]+)$') or ''] or nil
	end
	local lang = track.lang and languages[track.lang:lower()]
	if (not label or label == '') and lang then label = lang end
	if not label or label == '' then label = track.lang end
	if not label or label == '' then label = 'Track ' .. track.id end
	return label
end

local function count_tracks(kind)
	local n = 0
	for _, track in ipairs(state.tracks) do if track.type == kind then n = n + 1 end end
	return n
end

-- The episode button reads "Ep 13" when otakase named the entry "Episode 13".
local function episode_label()
	local title = entry_title(state.playlist_pos)
	local number = title and (title:match('[Ee]pisode%s*(%d+)') or title:match('[Ee]p%.?%s*(%d+)'))
	if number then return 'Ep ' .. number end
	if #state.playlist > 1 then return string.format('%d/%d', state.playlist_pos + 1, #state.playlist) end
	return nil
end

-- What the corner shows right now: a skip button, an undo notice, or the next
-- episode card. One at a time, in that order.
local function corner()
	local pos = state.pos
	if not pos then return nil end

	if state.notice and mp.get_time() < state.notice.until_time then
		return {kind = 'notice'}
	end
	state.notice = nil

	for _, span in ipairs({{'Opening', 'Skip Opening', opts.skip_op}, {'Ending', 'Skip Ending', opts.skip_ed}}) do
		local start, stop = chapter_span(span[1])
		if start and pos >= start and pos < stop - 1 then
			-- otakase skips this one itself as playback enters it. Only offer the
			-- button once it has let it play: the viewer seeked back into it.
			if not span[3] or pos >= start + AUTO_SKIP_WINDOW then
				return {kind = 'skip', label = span[2], to = stop}
			end
		end
	end

	local has_next, playlist_pos = playlist_has_next()
	if has_next and state.next_up_hidden_for ~= playlist_pos and state.duration then
		local ending = chapter_span('Ending')
		local from = ending or (state.duration - NEXT_UP_LEAD)
		if pos >= from and pos < state.duration then
			return {kind = 'next', remaining = math.max(0, state.duration - pos)}
		end
	end
	return nil
end

-- While the controls are up, subtitles move above them, as in the mockup,
-- and return when they hide. sub-pos is a percentage of the screen height
-- (100 is the bottom); sub-margin-y would not move them while paused.
local sub_lift = nil -- {base = sub-pos before, raised = what was set}
local function lift_subtitles(on, h, s)
	if on == (sub_lift ~= nil) then return end
	if on then
		local base = mp.get_property_number('sub-pos', 100)
		local raised = math.max(0, base - 92 * s / h * 100)
		sub_lift = {base = base, raised = raised}
		mp.set_property_number('sub-pos', raised)
	else
		-- A change the user made meanwhile (r, R) is kept, moved down with the rest.
		local now = mp.get_property_number('sub-pos', sub_lift.raised)
		mp.set_property_number('sub-pos', math.min(150, sub_lift.base + (now - sub_lift.raised)))
		sub_lift = nil
	end
end

local function controls_visible(w, h, s)
	if state.paused or state.menu or state.dragging then return true end
	if mp.get_time() < state.shown_until then return true end
	-- Kept up while the pointer rests on either bar.
	if state.mouse.hover and (state.mouse.y < 110 * s or state.mouse.y > h - 150 * s) then return true end
	return false
end

-- Menus ----------------------------------------------------------------------

local function open_menu(kind, anchor_x, anchor_y)
	local items = {}
	if kind == 'sub' or kind == 'audio' then
		if kind == 'sub' then
			items[#items + 1] = {label = 'Off', icon = 'close', selected = current_track('sub') == nil,
				run = function() mp.set_property('sid', 'no') end}
		end
		for _, track in ipairs(state.tracks) do
			if track.type == kind then
				local label = track_label(track)
				if track.title and track.title ~= '' and track.lang and track.lang ~= '' then
					label = track.title .. ' · ' .. track.lang
				end
				local id = track.id
				items[#items + 1] = {label = label, selected = track.selected,
					run = function() mp.set_property_number(kind == 'sub' and 'sid' or 'aid', id) end}
			end
		end
	elseif kind == 'episodes' then
		for i, entry in ipairs(state.playlist) do
			local index = i - 1
			items[#items + 1] = {label = entry.title and entry.title ~= '' and entry.title or ('Entry ' .. i),
				selected = index == state.playlist_pos,
				run = function() mp.set_property_number('playlist-pos', index) end}
		end
	end
	local scroll = 0
	for i, item in ipairs(items) do if item.selected then scroll = i - 1 end end
	state.menu = {kind = kind, items = items, scroll = scroll, anchor_x = anchor_x, anchor_y = anchor_y}
	request_render()
end

local function close_menu()
	state.menu = nil
	request_render()
end

local MENU_TITLES = {sub = {'Subtitles', 'subtitles'}, audio = {'Audio', 'record_voice_over'}, episodes = {'Episodes', 'playlist_play'}}

local function draw_menu(ass, w, h, s)
	local menu = state.menu
	local item_h = 38 * s
	local pad = 8 * s
	local head_h = 36 * s
	local width = 330 * s
	for _, item in ipairs(menu.items) do
		width = math.max(width, text_width(item.label, 14 * s) + 90 * s)
	end
	width = math.min(width, w - 40 * s)
	local max_rows = math.max(3, math.floor((menu.anchor_y - 40 * s - head_h - 2 * pad) / item_h))
	local rows = math.min(#menu.items, max_rows)
	-- Keep the chosen item in view, then honour the wheel.
	menu.scroll = math.max(0, math.min(menu.scroll, #menu.items - rows))
	if menu.first == nil then
		menu.first = math.max(0, math.min(menu.scroll - math.floor(rows / 2), #menu.items - rows))
	end
	menu.first = math.max(0, math.min(menu.first, #menu.items - rows))
	menu.rows = rows

	local bx = math.min(w - 20 * s, menu.anchor_x + width / 2)
	local ax = math.max(20 * s, bx - width)
	bx = ax + width
	local by = menu.anchor_y
	local ay = by - (head_h + rows * item_h + 2 * pad)
	ass[#ass + 1] = box(ax, ay, bx, by, 12 * s, C.bg, 0.95, C.surface, 1)
	hit('menu-body', ax, ay, bx, by, function() end)

	local title = MENU_TITLES[menu.kind]
	ass[#ass + 1] = icon(ax + pad + 12 * s, ay + pad + head_h / 2, title[2], 18 * s, C.dim)
	ass[#ass + 1] = text(ax + pad + 28 * s, ay + pad + head_h / 2, 4, title[1]:upper(), 12 * s, C.dim)

	for row = 1, rows do
		local index = menu.first + row
		local item = menu.items[index]
		local iy = ay + pad + head_h + (row - 1) * item_h
		local id = 'menu-item-' .. index
		local active = hovered == id
		if item.selected or active then
			ass[#ass + 1] = box(ax + pad, iy, bx - pad, iy + item_h, 8 * s, C.surface, active and 1 or 0.8)
		end
		local mark = item.selected and 'check' or item.icon
		if mark then
			ass[#ass + 1] = icon(ax + pad + 14 * s, iy + item_h / 2, mark, 18 * s, item.icon and not item.selected and C.dim or C.accent)
		end
		ass[#ass + 1] = text(ax + pad + 36 * s, iy + item_h / 2, 4, shorten(item.label, 60), 14 * s,
			(item.selected or active) and C.bright or C.fg)
		hit(id, ax + pad, iy, bx - pad, iy + item_h, function()
			item.run()
			close_menu()
		end)
	end
	if #menu.items > rows then
		local frac = menu.first / math.max(1, #menu.items - rows)
		local track_h = rows * item_h - 8 * s
		local thumb_h = math.max(20 * s, track_h * rows / #menu.items)
		local ty = ay + pad + head_h + 4 * s + (track_h - thumb_h) * frac
		ass[#ass + 1] = box(bx - 6 * s, ty, bx - 3 * s, ty + thumb_h, 2 * s, C.dim, 0.6)
	end
end

-- Rendering --------------------------------------------------------------------

local function draw_button(ass, id, cx, cy, icon_name, label, s, action, opts_)
	opts_ = opts_ or {}
	local size = 13 * s
	local icon_size = 22 * s
	local bw = label and (9 * s + icon_size + 8 * s + text_width(label, size) + 9 * s) or 40 * s
	local ax = opts_.right and (cx - bw) or cx
	local bx = ax + bw
	local ay, by = cy - 20 * s, cy + 20 * s
	local active = hovered == id or opts_.on
	if active or opts_.plate then
		ass[#ass + 1] = box(ax, ay, bx, by, 10 * s, C.surface, active and 0.9 or 0.6)
	end
	local color = (opts_.on and C.accent) or (hovered == id and C.bright) or C.fg
	if label then
		ass[#ass + 1] = icon(ax + 9 * s + icon_size / 2, cy, icon_name, icon_size, color)
		ass[#ass + 1] = text(ax + 9 * s + icon_size + 8 * s, cy, 4, label, size, hovered == id and C.bright or C.fg)
	else
		ass[#ass + 1] = icon((ax + bx) / 2, cy, icon_name, icon_size, color)
	end
	if action then hit(id, ax, ay, bx, by, action) end
	return ax, bx
end

local function seek_to_x(x, ax, bx, exact)
	if not state.duration or state.duration <= 0 then return end
	local t = math.max(0, math.min(1, (x - ax) / (bx - ax))) * state.duration
	mp.commandv('seek', tostring(t), exact and 'absolute+exact' or 'absolute+keyframes')
end

local seek_geometry = nil

local function chip_width(chip, s)
	return 10 * s + 16 * s + 6 * s + text_width(chip.text, 13 * s) + 12 * s
end

local function chips_width(s)
	local total = 0
	for _, chip in ipairs(state.chips) do total = total + chip_width(chip, s) + 8 * s end
	return total
end

-- value, cut with an ellipsis to fit in room.
local function fit(value, size, bold, room)
	if room <= 0 then return '' end
	if text_width(value, size, bold) <= room then return value end
	local chars = {}
	for ch in value:gmatch('[%z\1-\127\194-\244][\128-\191]*') do chars[#chars + 1] = ch end
	local lo, hi = 0, #chars
	while lo < hi do
		local mid = math.floor((lo + hi + 1) / 2)
		if text_width(table.concat(chars, '', 1, mid) .. '…', size, bold) <= room then lo = mid else hi = mid - 1 end
	end
	if lo == 0 then return '' end
	return (table.concat(chars, '', 1, lo):gsub('%s+$', '')) .. '…'
end

local function draw_controls(ass, w, h, s)
	-- Top bar.
	ass[#ass + 1] = fade(0, 0, w, 110 * s, C.bg, 0.92, 0)
	local title = state.title or ''
	local main, rest = title:match('^(.-)%s+[-–·]%s+(.+)$')
	if not main then main, rest = title, nil end
	local _, back_bx = draw_button(ass, 'back', 20 * s, 36 * s, 'arrow_back', nil, s, function()
		lift_subtitles(false)
		mp.command('quit')
	end, {plate = true})
	local tx = back_bx + 14 * s
	local ty = 36 * s
	local size = 16 * s
	local room = w - tx - 20 * s - chips_width(s) - 24 * s
	main = fit(main, size, true, room)
	local line = string.format('{\\pos(%.1f,%.1f)\\an4\\bord0\\shad0\\blur0\\fn%s\\fs%.1f\\b1\\1c&H%s&\\1a&H00&}%s',
		tx, ty, text_font(), size, C.bright, escape(main))
	if rest then
		rest = fit(rest, size, false, room - text_width(main, size, true) - text_width(' · ', size))
		if rest ~= '' then
			line = line .. string.format('{\\b0\\1c&H%s&} · {\\1c&H%s&}%s', C.dim, C.fg, escape(rest))
		end
	end
	ass[#ass + 1] = line

	-- Chips, right-aligned in the top bar.
	local x = w - 20 * s
	size = 13 * s
	local bh = 30 * s
	local cy = ty
	for i = #state.chips, 1, -1 do
		local chip = state.chips[i]
		local bw = chip_width(chip, s)
		ass[#ass + 1] = box(x - bw, cy - bh / 2, x, cy + bh / 2, 8 * s, C.surface, 0.8)
		ass[#ass + 1] = icon(x - bw + 10 * s + 8 * s, cy, chip.icon or 'circle', 16 * s, C.accent)
		ass[#ass + 1] = text(x - bw + 10 * s + 16 * s + 6 * s, cy, 4, chip.text, size, C.fg)
		x = x - bw - 8 * s
	end

	-- Bottom bar.
	ass[#ass + 1] = fade(0, h - 150 * s, w, h, C.bg, 0, 0.95)

	-- Seek bar.
	local ax, bx = 20 * s, w - 20 * s
	local sy = h - 86 * s
	seek_geometry = {ax = ax, bx = bx}
	local dur = state.duration
	if dur and dur > 0 then
		local function px(t) return ax + (bx - ax) * math.max(0, math.min(1, t / dur)) end
		local hovering = hovered == 'seek'
		local th = (hovering or state.dragging) and 6 * s or 4 * s
		ass[#ass + 1] = box(ax, sy - th / 2, bx, sy + th / 2, th / 2, C.fg, 0.28)
		if state.cache_end and state.pos and state.cache_end > state.pos then
			ass[#ass + 1] = box(ax, sy - th / 2, px(state.cache_end), sy + th / 2, th / 2, C.fg, 0.33)
		end
		local pos = state.pos or 0
		if pos > 0 then
			ass[#ass + 1] = box(ax, sy - th / 2, px(pos), sy + th / 2, th / 2, C.accent, 1)
		end
		-- Openings and endings sit on top of the fill, so they stay visible once played.
		for _, span in ipairs({{'Opening', 'OP'}, {'Ending', 'ED'}}) do
			local start, stop = chapter_span(span[1])
			if start then
				local cx0, cx1 = px(start), px(stop)
				ass[#ass + 1] = box(cx0, sy - th / 2 - 1 * s, cx1, sy + th / 2 + 1 * s, 3 * s, C.highlight, 1, C.bg, 0.5)
				ass[#ass + 1] = text(cx0, sy - 12 * s, 1, span[2], 11 * s, C.highlight)
			end
		end
		ass[#ass + 1] = circle(px(pos), sy, 8 * s, C.bright, 1, C.accent, 4 * s, 0.35)
		hit('seek', ax, sy - 12 * s, bx, sy + 12 * s, function() end, {seek = true})

		if (hovering or state.dragging) and state.mouse.x >= ax then
			local t = math.max(0, math.min(1, (state.mouse.x - ax) / (bx - ax))) * dur
			local label = clock(t)
			local chapter = chapter_at(t)
			if chapter == 'Opening' then label = label .. ' · OP' elseif chapter == 'Ending' then label = label .. ' · ED' end
			local lw = text_width(label, 12 * s) + 16 * s
			local lx = math.max(ax, math.min(bx - lw, state.mouse.x - lw / 2))
			ass[#ass + 1] = box(lx, sy - 50 * s, lx + lw, sy - 26 * s, 6 * s, C.bg, 0.95)
			ass[#ass + 1] = text(lx + lw / 2, sy - 38 * s, 5, label, 12 * s, C.bright)
		end
	end

	-- Button row.
	local ry = h - 40 * s
	local x0 = 20 * s
	local _, e = draw_button(ass, 'prev', x0, ry, 'skip_previous', nil, s, function() mp.command('playlist-prev') end)
	local pcx = e + 30 * s
	local play_id = 'play'
	ass[#ass + 1] = circle(pcx, ry, 24 * s, C.accent, hovered == play_id and 1 or 0.95)
	ass[#ass + 1] = icon(pcx, ry, state.paused and 'play_arrow' or 'pause', 28 * s, C.accent_text)
	hit(play_id, pcx - 24 * s, ry - 24 * s, pcx + 24 * s, ry + 24 * s, function() mp.command('cycle pause') end)
	_, e = draw_button(ass, 'next', pcx + 30 * s, ry, 'skip_next', nil, s, function() mp.command('playlist-next') end)
	local time_x = e + 12 * s
	local now = clock(state.pos)
	local total = dur and (' / ' .. clock(dur)) or ''
	ass[#ass + 1] = text(time_x, ry, 4, now, 14 * s, C.bright) .. string.format('{\\1c&H%s&}%s', C.dim, total)
	local vx = time_x + text_width(now .. total, 14 * s) + 24 * s

	-- Right group, laid out from the right edge.
	local rx = w - 20 * s
	local function right(id, icon_name, label, action, on)
		local a = draw_button(ass, id, rx, ry, icon_name, label, s, action, {right = true, on = on})
		rx = a - 6 * s
		return a
	end
	right('fullscreen', state.fullscreen and 'fullscreen_exit' or 'fullscreen', nil, function() mp.command('cycle fullscreen') end)
	local narrow = w < 900 * s
	if state.height and not narrow then
		right('quality', 'tune', state.height .. 'p', nil)
	end
	if count_tracks('sub') > 0 then
		local label = narrow and nil or (track_label(current_track('sub')) or 'Off')
		local a = right('sub', current_track('sub') and 'subtitles' or 'subtitles_off', label and shorten(label, 14),
			function() if state.menu and state.menu.kind == 'sub' then close_menu() else open_menu('sub', rx, h - 72 * s) end end,
			state.menu and state.menu.kind == 'sub')
		if state.menu and state.menu.kind == 'sub' then state.menu.anchor_x = a + 40 * s end
	end
	if count_tracks('audio') > 1 then
		local label = narrow and nil or track_label(current_track('audio'))
		local a = right('audio', 'record_voice_over', label and shorten(label, 12),
			function() if state.menu and state.menu.kind == 'audio' then close_menu() else open_menu('audio', rx, h - 72 * s) end end,
			state.menu and state.menu.kind == 'audio')
		if state.menu and state.menu.kind == 'audio' then state.menu.anchor_x = a + 40 * s end
	end
	if #state.playlist > 1 then
		local a = right('episodes', 'playlist_play', episode_label(),
			function() if state.menu and state.menu.kind == 'episodes' then close_menu() else open_menu('episodes', rx, h - 72 * s) end end,
			state.menu and state.menu.kind == 'episodes')
		if state.menu and state.menu.kind == 'episodes' then state.menu.anchor_x = a + 40 * s end
	end

	-- Volume, if there is room between the time and the right group.
	if vx + 120 * s < rx then
		local vol_icon = (state.muted or state.volume <= 0) and 'volume_off' or (state.volume < 50 and 'volume_down' or 'volume_up')
		ass[#ass + 1] = icon(vx + 10 * s, ry, vol_icon, 20 * s, hovered == 'mute' and C.bright or C.fg)
		hit('mute', vx - 4 * s, ry - 16 * s, vx + 24 * s, ry + 16 * s, function() mp.command('cycle mute') end)
		local vax, vbx = vx + 30 * s, vx + 110 * s
		local frac = math.max(0, math.min(1, state.volume / 100))
		ass[#ass + 1] = box(vax, ry - 2 * s, vbx, ry + 2 * s, 2 * s, C.fg, 0.2)
		ass[#ass + 1] = box(vax, ry - 2 * s, vax + (vbx - vax) * frac, ry + 2 * s, 2 * s, state.muted and C.dim or C.fg, 1)
		hit('volume', vax - 4 * s, ry - 12 * s, vbx + 4 * s, ry + 12 * s, function()
			local v = math.max(0, math.min(1, (state.mouse.x - vax) / (vbx - vax))) * 100
			mp.set_property_number('volume', math.floor(v + 0.5))
		end, {wheel = 'volume'})
	end
end

local function draw_progress_line(ass, w, h, s)
	local dur = state.duration
	if not dur or dur <= 0 or not state.pos then return end
	local lh = math.max(2, 3 * s)
	ass[#ass + 1] = box(0, h - lh, w, h, 0, C.fg, 0.25)
	ass[#ass + 1] = box(0, h - lh, w * math.min(1, state.pos / dur), h, 0, C.accent, 1)
end

local function draw_corner(ass, w, h, s, visible)
	local right = w - 28 * s
	local bottom = visible and (h - 166 * s) or (h - 70 * s)
	local c = corner()
	enter_action = nil

	if c and c.kind == 'skip' then
		local size = 15 * s
		local bh = 44 * s
		local bw = 16 * s + 20 * s + 10 * s + text_width(c.label, size) + 10 * s + text_width('Enter', 11 * s) + 12 * s + 16 * s
		local ax, ay = right - bw, bottom - bh
		local active = hovered == 'skip'
		ass[#ass + 1] = box(ax, ay, right, bottom, 12 * s, active and C.accent or C.bg, active and 1 or 0.9, C.accent, 0.55)
		ass[#ass + 1] = icon(ax + 16 * s + 10 * s, ay + bh / 2, 'skip_next', 22 * s, active and C.accent_text or C.accent)
		ass[#ass + 1] = text(ax + 16 * s + 20 * s + 10 * s, ay + bh / 2, 4, c.label, size, active and C.accent_text or C.bright)
		local kx = right - 16 * s - text_width('Enter', 11 * s) - 12 * s
		ass[#ass + 1] = box(kx, ay + bh / 2 - 10 * s, right - 16 * s, ay + bh / 2 + 10 * s, 5 * s, C.bg, 0, active and C.accent_text or C.dim, 0.6)
		ass[#ass + 1] = text(kx + 6 * s, ay + bh / 2, 4, 'Enter', 11 * s, active and C.accent_text or C.dim)
		local to = c.to
		hit('skip', ax, ay, right, bottom, function() mp.commandv('seek', tostring(to), 'absolute+exact') end)
		enter_action = function() mp.commandv('seek', tostring(to), 'absolute+exact') end
	elseif c and c.kind == 'notice' then
		local size = 14 * s
		local label = state.notice.label
		local bh = 44 * s
		local undo_w = 10 * s + 18 * s + 8 * s + text_width('Undo', size) + 12 * s
		local bw = 16 * s + text_width(label, size) + 14 * s + undo_w + 6 * s
		local ax, ay = right - bw, bottom - bh
		ass[#ass + 1] = box(ax, ay, right, bottom, 12 * s, C.bg, 0.9, C.surface, 1)
		ass[#ass + 1] = text(ax + 16 * s, ay + bh / 2, 4, label, size, C.fg)
		local ux = right - 6 * s - undo_w
		local active = hovered == 'undo'
		ass[#ass + 1] = box(ux, ay + 6 * s, right - 6 * s, bottom - 6 * s, 8 * s, active and C.accent or C.surface, 1)
		ass[#ass + 1] = icon(ux + 10 * s + 9 * s, ay + bh / 2, 'undo', 18 * s, active and C.accent_text or C.accent)
		ass[#ass + 1] = text(ux + 10 * s + 18 * s + 8 * s, ay + bh / 2, 4, 'Undo', size, active and C.accent_text or C.bright)
		local function undo()
			if state.notice then
				mp.commandv('seek', tostring(state.notice.undo_to), 'absolute+exact')
				state.notice = nil
			end
		end
		hit('undo', ux, ay + 6 * s, right - 6 * s, bottom - 6 * s, undo)
		enter_action = undo
	elseif c and c.kind == 'next' then
		local size = 14 * s
		local small = 12 * s
		local title = shorten(entry_title(state.playlist_pos + 1) or 'Next episode', 44)
		local heading = string.format('Next episode in %ds', math.floor(c.remaining + 0.5))
		local play_w = 12 * s + 16 * s + 6 * s + text_width('Play now', 13 * s) + 12 * s
		local cancel_w = 12 * s + text_width('Cancel', 13 * s) + 12 * s
		local bw = math.max(text_width(title, size), text_width(heading, small), play_w + 8 * s + cancel_w) + 28 * s
		local bh = 100 * s
		local ax, ay = right - bw, bottom - bh
		ass[#ass + 1] = box(ax, ay, right, bottom, 12 * s, C.bg, 0.95, C.surface, 1)
		ass[#ass + 1] = text(ax + 14 * s, ay + 22 * s, 4, heading, small, C.dim)
		ass[#ass + 1] = text(ax + 14 * s, ay + 44 * s, 4, title, size, C.bright, 1, true)
		local py = ay + 62 * s
		local play_active = hovered == 'next'
		ass[#ass + 1] = box(ax + 14 * s, py, ax + 14 * s + play_w, py + 28 * s, 8 * s, C.accent, play_active and 1 or 0.9)
		ass[#ass + 1] = icon(ax + 14 * s + 12 * s + 8 * s, py + 14 * s, 'play_arrow', 18 * s, C.accent_text)
		ass[#ass + 1] = text(ax + 14 * s + 12 * s + 16 * s + 6 * s, py + 14 * s, 4, 'Play now', 13 * s, C.accent_text)
		hit('next', ax + 14 * s, py, ax + 14 * s + play_w, py + 28 * s, function() mp.commandv('playlist-next', 'force') end)
		local cx = ax + 14 * s + play_w + 8 * s
		local cancel_active = hovered == 'dismiss'
		ass[#ass + 1] = box(cx, py, cx + cancel_w, py + 28 * s, 8 * s, C.surface, cancel_active and 1 or 0.8)
		ass[#ass + 1] = text(cx + 12 * s, py + 14 * s, 4, 'Cancel', 13 * s, cancel_active and C.bright or C.fg)
		local function dismiss()
			local _, pos = playlist_has_next()
			state.next_up_hidden_for = pos
		end
		hit('dismiss', cx, py, cx + cancel_w, py + 28 * s, dismiss)
		enter_action = function() mp.commandv('playlist-next', 'force') end
	end
end

local update_bindings

local function render()
	local w, h = mp.get_osd_size()
	if not w or w <= 0 or h <= 0 then return end
	local s = scale(h)
	set_measure_size(w, h)
	local ass = {}
	hits = {}
	seek_geometry = nil

	local visible = controls_visible(w, h, s)
	lift_subtitles(visible, h, s)
	if visible then
		draw_controls(ass, w, h, s)
	else
		draw_progress_line(ass, w, h, s)
	end
	-- An open menu covers the corner, so the corner waits until it closes.
	if state.menu then
		enter_action = nil
		draw_menu(ass, w, h, s)
	else
		draw_corner(ass, w, h, s, visible)
	end

	-- time-pos changes every frame; most frames draw the same thing.
	local data = table.concat(ass, '\n')
	if data ~= overlay.data or w ~= overlay.res_x or h ~= overlay.res_y then
		overlay.res_x, overlay.res_y = w, h
		overlay.data = data
		overlay:update()
	end
	update_bindings()
end

local render_timer = nil
request_render = function()
	if render_timer then return end
	render_timer = mp.add_timeout(0.016, function()
		render_timer = nil
		render()
	end)
end

-- Input ----------------------------------------------------------------------

local function hit_at(x, y)
	-- Last drawn is on top: menus and the corner come after the bars.
	for i = #hits, 1, -1 do
		local h = hits[i]
		if x >= h.ax and x <= h.bx and y >= h.ay and y <= h.by then return h end
	end
end

local function on_click(event)
	if event.event == 'down' or event.event == 'press' then
		local target = state.mouse.hover and hit_at(state.mouse.x, state.mouse.y) or nil
		if state.menu and (not target or not target.id:match('^menu')) then
			-- A click outside an open menu closes it, unless it is on the button
			-- that opened it, which toggles it below.
			local opener = target and (target.id == state.menu.kind)
			if not opener then
				close_menu()
				return
			end
		end
		if not target then return end
		if target.seek and seek_geometry then
			state.dragging = event.event == 'down'
			seek_to_x(state.mouse.x, seek_geometry.ax, seek_geometry.bx, true)
		elseif target.action then
			target.action()
		end
		state.shown_until = mp.get_time() + opts.hide_after
		request_render()
	elseif event.event == 'up' then
		if state.dragging and seek_geometry then
			seek_to_x(state.mouse.x, seek_geometry.ax, seek_geometry.bx, true)
		end
		state.dragging = false
		request_render()
	end
end

local function on_wheel(direction)
	return function()
		if state.menu then
			state.menu.first = (state.menu.first or 0) + direction * 2
			request_render()
			return
		end
		local target = hit_at(state.mouse.x, state.mouse.y)
		if target and target.wheel == 'volume' then
			mp.commandv('add', 'volume', tostring(-direction * 5))
		end
	end
end

local function bind(name, key, fn, flags)
	if bound[name] then return end
	mp.add_forced_key_binding(key, name, fn, flags)
	bound[name] = true
end

local function unbind(name)
	if not bound[name] then return end
	mp.remove_key_binding(name)
	bound[name] = nil
end

-- The click and wheel bindings exist only while the pointer is over our
-- controls (or a menu is open), so mpv's own double click to fullscreen and
-- window dragging work everywhere else.
update_bindings = function()
	local target = state.mouse.hover and hit_at(state.mouse.x, state.mouse.y) or nil
	local id = target and target.id or nil
	if id ~= hovered then
		hovered = id
		request_render()
	end
	if target or state.menu or state.dragging then
		bind('otakase-skin-click', 'MBTN_LEFT', on_click, {complex = true})
	else
		unbind('otakase-skin-click')
	end
	if state.menu or (target and target.wheel) then
		bind('otakase-skin-wheel-up', 'WHEEL_UP', on_wheel(-1))
		bind('otakase-skin-wheel-down', 'WHEEL_DOWN', on_wheel(1))
	else
		unbind('otakase-skin-wheel-up')
		unbind('otakase-skin-wheel-down')
	end
	-- Enter acts on whatever the corner offers, only while it offers something.
	if enter_action and not state.menu then
		bind('otakase-skin-enter', 'ENTER', function()
			if enter_action then enter_action() end
			request_render()
		end)
	else
		unbind('otakase-skin-enter')
	end
	if state.menu then
		bind('otakase-skin-esc', 'ESC', close_menu)
	else
		unbind('otakase-skin-esc')
	end
end

-- otakase skips by seeking from the first seconds of a span to its end. Seeing
-- that jump is how the undo notice knows a skip happened.
local function on_seek_from(previous)
	-- Any later jump means the viewer has moved on from the last notice.
	state.notice = nil
	if not previous or not state.pos then return end
	for _, span in ipairs({{'Opening', 'Skipped Opening', opts.skip_op}, {'Ending', 'Skipped Ending', opts.skip_ed}}) do
		local start, stop = chapter_span(span[1])
		if span[3] and start and previous >= start - 0.5 and previous < start + AUTO_SKIP_WINDOW
			and math.abs(state.pos - stop) < 1.5 then
			state.notice = {
				label = span[2],
				until_time = mp.get_time() + NOTICE_SECONDS,
				undo_to = start + AUTO_SKIP_WINDOW,
			}
			return
		end
	end
end

-- Properties -----------------------------------------------------------------

local last_pos = nil
mp.observe_property('time-pos', 'number', function(_, value)
	local previous = last_pos
	state.pos = value
	last_pos = value
	if previous and value and math.abs(value - previous) > 3 then on_seek_from(previous) end
	request_render()
end)
mp.observe_property('duration', 'number', function(_, value) state.duration = value; request_render() end)
mp.observe_property('demuxer-cache-time', 'number', function(_, value) state.cache_end = value end)
mp.observe_property('chapter-list', 'native', function(_, value)
	state.chapters = value or {}
	table.sort(state.chapters, function(a, b) return a.time < b.time end)
	request_render()
end)
mp.observe_property('pause', 'bool', function(_, value) state.paused = value; request_render() end)
mp.observe_property('fullscreen', 'bool', function(_, value) state.fullscreen = value; request_render() end)
mp.observe_property('volume', 'number', function(_, value) state.volume = value or 100; request_render() end)
mp.observe_property('mute', 'bool', function(_, value) state.muted = value; request_render() end)
mp.observe_property('media-title', 'string', function(_, value) state.title = value or ''; request_render() end)
mp.observe_property('height', 'number', function(_, value) state.height = value and math.floor(value) or nil; request_render() end)
mp.observe_property('track-list', 'native', function(_, value) state.tracks = value or {}; request_render() end)
mp.observe_property('playlist', 'native', function(_, value) state.playlist = value or {}; request_render() end)
mp.observe_property('playlist-pos', 'number', function(_, value)
	state.playlist_pos = value or -1
	state.notice = nil
	request_render()
end)
mp.observe_property('osd-dimensions', 'native', function() request_render() end)
mp.observe_property('mouse-pos', 'native', function(_, value)
	if not value then return end
	local moved = value.x ~= state.mouse.x or value.y ~= state.mouse.y
	state.mouse = {x = value.x, y = value.y, hover = value.hover}
	if moved and value.hover then state.shown_until = mp.get_time() + opts.hide_after end
	if state.dragging and seek_geometry then
		seek_to_x(value.x, seek_geometry.ax, seek_geometry.bx, false)
	end
	request_render()
end)

-- Keyboard seeks flash the controls, as mpv's own bar would.
mp.register_event('seek', function()
	state.shown_until = mp.get_time() + 1.0
	request_render()
end)

-- Hiding and the notice timing out happen with nothing else changing.
mp.add_periodic_timer(0.25, request_render)

-- otakase sends {"chips": [{"icon": "sync", "text": "AniList"}, ...]}.
mp.register_script_message('otakase-state', function(json)
	local data = utils.parse_json(json or '')
	if type(data) ~= 'table' then return end
	local chips = {}
	if type(data.chips) == 'table' then
		for _, chip in ipairs(data.chips) do
			if type(chip) == 'table' and type(chip.text) == 'string' and chip.text ~= '' then
				chips[#chips + 1] = {icon = type(chip.icon) == 'string' and chip.icon or nil, text = chip.text}
			end
		end
	end
	state.chips = chips
	request_render()
end)
