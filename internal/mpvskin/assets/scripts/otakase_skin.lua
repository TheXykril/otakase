--[[
otakase's own additions to the player, drawn next to uosc:

  * a "Skip Opening" / "Skip Ending" button while one plays and otakase is
    not skipping it already,
  * a "Skipped Opening · Undo" notice when otakase did skip it,
  * an "Up next" card during the ending, while the playlist has a next episode,
  * small chips under the top bar naming the tracker and the source.

Openings and endings are the "Opening" and "Ending" chapters otakase writes
into the chapter list. Everything else arrives as script-opts at launch or as
an `otakase-state` message from otakase.
]]

local mp = require('mp')
local utils = require('mp.utils')
local options = require('mp.options')

local opts = {
	icon_font = 'Material Icons Round',
	background = '000000',
	foreground = 'E6E6FA',
	dim = '9A9A9A',
	accent = '7CB9E8',
	accent_text = '000000',
	skip_op = false,
	skip_ed = false,
}
options.read_options(opts, 'otakase_skin')

-- otakase skips a span only when it sees playback within its first two seconds,
-- and checks once a second. Seeking back to here is past that window, so undo
-- is not skipped again.
local AUTO_SKIP_WINDOW = 2.5
local NOTICE_SECONDS = 5
local NEXT_UP_LEAD = 30

local overlay = mp.create_osd_overlay('ass-events')
local state = {
	pos = nil,
	duration = nil,
	chapters = {},
	fullscreen = false,
	maximized = false,
	hidpi = 1,
	mouse = {x = -1, y = -1, hover = false},
	notice = nil,       -- {label, until_time, undo_to}
	next_up_hidden_for = nil, -- playlist position the card was dismissed on
	chips = {},
}
local buttons = {}   -- rectangles drawn this frame: {ax, ay, bx, by, action}
local hovered = nil
local bound_click = false
local bound_enter = false
local enter_action = nil
local update_bindings

-- Colours ------------------------------------------------------------------

-- ASS writes colours as BBGGRR.
local function ass_color(hex)
	hex = (hex or ''):gsub('^#', '')
	if #hex ~= 6 then return 'FFFFFF' end
	return hex:sub(5, 6) .. hex:sub(3, 4) .. hex:sub(1, 2)
end

local function ass_alpha(opacity)
	return string.format('%02X', math.floor((1 - opacity) * 255 + 0.5))
end

local C = {
	bg = ass_color(opts.background),
	fg = ass_color(opts.foreground),
	dim = ass_color(opts.dim),
	accent = ass_color(opts.accent),
	accent_text = ass_color(opts.accent_text),
}

local function text_font()
	return mp.get_property('options/osd-font') or 'sans-serif'
end

-- Drawing --------------------------------------------------------------------

local function rounded_rect(ax, ay, bx, by, r)
	r = math.min(r, (bx - ax) / 2, (by - ay) / 2)
	local k = r * 0.4477 -- (1 - 0.5523), bezier circle approximation
	return string.format(
		'm %d %d l %d %d b %d %d %d %d %d %d l %d %d b %d %d %d %d %d %d l %d %d b %d %d %d %d %d %d l %d %d b %d %d %d %d %d %d',
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

-- Titles come from providers; a brace in one would open an ASS tag. ASS has no
-- escape for a backslash, so one is followed by a zero-width space instead.
local function escape(value)
	value = tostring(value):gsub('\\', '\\\239\187\191')
	value = value:gsub('{', '\\{'):gsub('}', '\\}'):gsub('\n', ' ')
	return value
end

local function text(x, y, align, value, size, color, opacity, font, bold)
	value = escape(value)
	return string.format('{\\pos(%d,%d)\\an%d\\bord0\\shad0\\blur0\\fn%s\\fs%d\\b%d\\1c&H%s&\\1a&H%s&}%s',
		x, y, align, font or text_font(), size, bold and 1 or 0, color, ass_alpha(opacity or 1), value)
end

local function icon(x, y, name, size, color, opacity)
	return text(x, y, 5, name, size, color, opacity, opts.icon_font, false)
end

-- Rough width of a label: libass can't be asked, and the font is often
-- monospace anyway. Wide enough for proportional fonts too.
local function text_width(value, size)
	return #value * size * 0.6
end

-- Layout ---------------------------------------------------------------------

local function scale()
	local s = state.hidpi or 1
	if state.fullscreen or state.maximized then s = s * 1.3 end
	return s
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

local function playlist_has_next()
	local pos = mp.get_property_number('playlist-pos', -1)
	local count = mp.get_property_number('playlist-count', 0)
	return pos >= 0 and pos + 1 < count, pos
end

-- otakase names every episode in its playlist. Anything else is a file or a
-- link, which reads worse than no name at all.
local function next_title()
	local pos = mp.get_property_number('playlist-pos', -1)
	local title = mp.get_property('playlist/' .. (pos + 1) .. '/title') or ''
	if title == '' then return 'Next episode' end
	if #title > 48 then title = title:sub(1, 45) .. '...' end
	return title
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

-- The chips belong with uosc's top bar, which shows while the pointer is near
-- the top of the window.
local function chips_visible(s)
	return state.mouse.hover and state.mouse.y >= 0 and state.mouse.y < 200 * s
end

local render_timer

local function render()
	local w, h = mp.get_osd_size()
	if not w or w <= 0 or h <= 0 then return end
	local s = scale()
	local ass = {}
	buttons = {}

	-- The corner sits above uosc's timeline and control bar.
	local right = w - math.floor(24 * s)
	local bottom = h - math.floor(120 * s)
	local c = corner()

	if c and c.kind == 'skip' then
		local size = math.floor(15 * s)
		local label_w = text_width(c.label, size)
		local bh = math.floor(44 * s)
		local bw = math.floor(16 * s + 22 * s + 10 * s + label_w + 16 * s)
		local ax, ay = right - bw, bottom - bh
		local active = hovered == 'skip'
		ass[#ass + 1] = box(ax, ay, right, bottom, 12 * s, active and C.accent or C.bg, active and 1 or 0.9, C.accent, 0.5)
		ass[#ass + 1] = icon(ax + 16 * s + 11 * s, ay + bh / 2, 'skip_next', 22 * s, active and C.accent_text or C.accent)
		ass[#ass + 1] = text(ax + 16 * s + 22 * s + 10 * s, ay + bh / 2, 4, c.label, size, active and C.accent_text or C.fg)
		buttons[#buttons + 1] = {ax, ay, right, bottom, 'skip', to = c.to}
		enter_action = {'skip', to = c.to}
	elseif c and c.kind == 'notice' then
		local size = math.floor(14 * s)
		local label = state.notice.label
		local undo = 'Undo'
		local bh = math.floor(40 * s)
		local undo_w = math.floor(text_width(undo, size) + 24 * s + 20 * s)
		local bw = math.floor(16 * s + text_width(label, size) + 12 * s + undo_w + 6 * s)
		local ax, ay = right - bw, bottom - bh
		ass[#ass + 1] = box(ax, ay, right, bottom, 12 * s, C.bg, 0.9)
		ass[#ass + 1] = text(ax + 16 * s, ay + bh / 2, 4, label, size, C.fg)
		local ux = right - 6 * s - undo_w
		local active = hovered == 'undo'
		ass[#ass + 1] = box(ux, ay + 6 * s, right - 6 * s, bottom - 6 * s, 8 * s, active and C.accent or C.fg, active and 1 or 0.12)
		ass[#ass + 1] = icon(ux + 10 * s + 10 * s, ay + bh / 2, 'undo', 18 * s, active and C.accent_text or C.accent)
		ass[#ass + 1] = text(ux + 34 * s, ay + bh / 2, 4, undo, size, active and C.accent_text or C.fg)
		buttons[#buttons + 1] = {ux, ay + 6 * s, right - 6 * s, bottom - 6 * s, 'undo'}
		enter_action = {'undo'}
	elseif c and c.kind == 'next' then
		local size = math.floor(14 * s)
		local small = math.floor(12 * s)
		local title = next_title()
		local heading = string.format('Up next in %d s', math.floor(c.remaining + 0.5))
		local play = 'Play now'
		local play_w = math.floor(12 * s + 18 * s + 6 * s + text_width(play, small) + 12 * s)
		local bw = math.floor(math.max(text_width(title, size), text_width(heading, small), play_w + 40 * s) + 28 * s)
		local bh = math.floor(96 * s)
		local ax, ay = right - bw, bottom - bh
		ass[#ass + 1] = box(ax, ay, right, bottom, 12 * s, C.bg, 0.92)
		ass[#ass + 1] = text(ax + 14 * s, ay + 22 * s, 4, heading, small, C.dim)
		ass[#ass + 1] = text(ax + 14 * s, ay + 44 * s, 4, title, size, C.fg, 1, nil, true)
		local py = ay + 62 * s
		local active = hovered == 'next'
		ass[#ass + 1] = box(ax + 14 * s, py, ax + 14 * s + play_w, py + 24 * s, 8 * s, C.accent, active and 1 or 0.9)
		ass[#ass + 1] = icon(ax + 14 * s + 12 * s + 9 * s, py + 12 * s, 'play_arrow', 18 * s, C.accent_text)
		ass[#ass + 1] = text(ax + 14 * s + 12 * s + 18 * s + 6 * s, py + 12 * s, 4, play, small, C.accent_text)
		buttons[#buttons + 1] = {ax + 14 * s, py, ax + 14 * s + play_w, py + 24 * s, 'next'}
		local cx = right - 18 * s
		local cy = ay + 20 * s
		local close_active = hovered == 'dismiss'
		ass[#ass + 1] = icon(cx, cy, 'close', 18 * s, close_active and C.fg or C.dim)
		buttons[#buttons + 1] = {cx - 14 * s, cy - 14 * s, cx + 14 * s, cy + 14 * s, 'dismiss'}
		enter_action = {'next'}
	else
		enter_action = nil
	end

	-- Chips under the top bar, shown with the rest of the controls.
	if #state.chips > 0 and chips_visible(s) then
		local size = math.floor(12 * s)
		local x = right
		local ay = math.floor(48 * s)
		local bh = math.floor(26 * s)
		for i = #state.chips, 1, -1 do
			local chip = state.chips[i]
			local bw = math.floor(10 * s + 16 * s + 6 * s + text_width(chip.text, size) + 10 * s)
			ass[#ass + 1] = box(x - bw, ay, x, ay + bh, 8 * s, C.bg, 0.75)
			ass[#ass + 1] = icon(x - bw + 10 * s + 8 * s, ay + bh / 2, chip.icon or 'circle', 16 * s, C.accent)
			ass[#ass + 1] = text(x - bw + 10 * s + 16 * s + 6 * s, ay + bh / 2, 4, chip.text, size, C.fg)
			x = x - bw - math.floor(8 * s)
		end
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

local function request_render()
	if render_timer then render_timer:kill() end
	render_timer = mp.add_timeout(0.01, render)
end

-- Input ----------------------------------------------------------------------

local function run(action)
	if not action then return end
	local name = action[1]
	if name == 'skip' then
		mp.commandv('seek', tostring(action.to), 'absolute+exact')
	elseif name == 'undo' and state.notice then
		mp.commandv('seek', tostring(state.notice.undo_to), 'absolute+exact')
		state.notice = nil
	elseif name == 'next' then
		mp.commandv('playlist-next', 'force')
	elseif name == 'dismiss' then
		local _, pos = playlist_has_next()
		state.next_up_hidden_for = pos
	end
	hovered = nil
	request_render()
end

local function button_at(x, y)
	for _, b in ipairs(buttons) do
		if x >= b[1] and x <= b[3] and y >= b[2] and y <= b[4] then return b end
	end
end

-- The click binding exists only while the pointer is over one of our buttons,
-- so uosc gets every other click.
update_bindings = function()
	local b = state.mouse.hover and button_at(state.mouse.x, state.mouse.y) or nil
	local name = b and b[5] or nil
	if name ~= hovered then
		hovered = name
		request_render()
	end
	if b and not bound_click then
		mp.add_forced_key_binding('MBTN_LEFT', 'otakase-skin-click', function()
			local target = button_at(state.mouse.x, state.mouse.y)
			if target then run({target[5], to = target.to}) end
		end)
		bound_click = true
	elseif not b and bound_click then
		mp.remove_key_binding('otakase-skin-click')
		bound_click = false
	end
	-- Enter acts on whatever the corner offers, only while it offers something.
	if enter_action and not bound_enter then
		mp.add_forced_key_binding('ENTER', 'otakase-skin-enter', function() run(enter_action) end)
		bound_enter = true
	elseif not enter_action and bound_enter then
		mp.remove_key_binding('otakase-skin-enter')
		bound_enter = false
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
			request_render()
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
mp.observe_property('chapter-list', 'native', function(_, value)
	state.chapters = value or {}
	table.sort(state.chapters, function(a, b) return a.time < b.time end)
	request_render()
end)
mp.observe_property('fullscreen', 'bool', function(_, value) state.fullscreen = value; request_render() end)
mp.observe_property('window-maximized', 'bool', function(_, value) state.maximized = value; request_render() end)
mp.observe_property('display-hidpi-scale', 'number', function(_, value) state.hidpi = value or 1; request_render() end)
mp.observe_property('osd-dimensions', 'native', function() request_render() end)
mp.observe_property('playlist-pos', 'number', function()
	state.notice = nil
	request_render()
end)
mp.observe_property('mouse-pos', 'native', function(_, value)
	if not value then return end
	state.mouse = {x = value.x, y = value.y, hover = value.hover}
	request_render()
end)

-- The notice times out with nothing else changing.
mp.add_periodic_timer(0.5, function()
	if state.notice then request_render() end
end)

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
