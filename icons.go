package main

import "github.com/egoist/mygo/ui"

func svgIcon(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.85" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

var (
	iconRect = svgIcon(`<rect x="4" y="5" width="16" height="14" rx="2.5"/>`)

	iconEllipse = svgIcon(`<ellipse cx="12" cy="12" rx="8.5" ry="6.5"/>`)

	iconArrow = svgIcon(`<path d="M5 19L19 5"/><path d="M9.5 5H19V14.5"/>`)

	iconHighlighter = svgIcon(`<path d="M14.5 4.5l5 5-9.5 9.5H5v-5L14.5 4.5z"/><path d="M4 21h16"/>`)

	iconStep = svgIcon(`<circle cx="12" cy="12" r="8.5"/><path d="M10.5 9.5L12.3 8v8"/><path d="M10.2 16h4.2"/>`)

	iconMosaic = svgIcon(`<rect x="4" y="4" width="16" height="16" rx="2"/><path d="M4 9.33h16M4 14.67h16M9.33 4v16M14.67 4v16"/>`)

	iconText = svgIcon(`<path d="M5 6V4.5h14V6"/><path d="M12 4.5v15"/><path d="M9 19.5h6"/>`)

	iconUndo = svgIcon(`<path d="M9 14L4 9l5-5"/><path d="M4 9h10.5a5.5 5.5 0 0 1 0 11H11"/>`)

	iconWindowOnly = svgIcon(`<rect x="3.5" y="4.5" width="17" height="15" rx="2.5"/><path d="M3.5 9h17"/><circle cx="6.8" cy="6.8" r="0.7" fill="currentColor"/><circle cx="9.5" cy="6.8" r="0.7" fill="currentColor"/>`)

	iconRuler = svgIcon(`<path d="M3.5 16.5L16.5 3.5l4 4L7.5 20.5l-4-4z"/><path d="M13 7l2 2M10 10l1.5 1.5M7 13l2 2"/>`)

	iconOCR = svgIcon(`<path d="M4 8V5.5A1.5 1.5 0 0 1 5.5 4H8"/><path d="M16 4h2.5A1.5 1.5 0 0 1 20 5.5V8"/><path d="M20 16v2.5a1.5 1.5 0 0 1-1.5 1.5H16"/><path d="M8 20H5.5A1.5 1.5 0 0 1 4 18.5V16"/><path d="M8 9.5h8M8 12.5h8M8 15.5h5"/>`)

	iconPin = svgIcon(`<path d="M15 4.5l4.5 4.5-2.8 2.8.8 4.2-4.5-4.5L7.5 17l-.5-.5 5.5-5.5-4.5-4.5 4.2.8L15 4.5z"/>`)

	iconSave = svgIcon(`<path d="M12 4v11"/><path d="M7.5 11L12 15.5 16.5 11"/><path d="M4.5 19.5h15"/>`)

	iconClose = svgIcon(`<path d="M6.5 6.5l11 11M17.5 6.5l-11 11"/>`)

	iconCopy = svgIcon(`<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15H4.5A1.5 1.5 0 0 1 3 13.5v-9A1.5 1.5 0 0 1 4.5 3h9A1.5 1.5 0 0 1 15 4.5V5"/>`)

	iconCheck = svgIcon(`<path d="M5 12.5l4.5 4.5L19 7.5"/>`)

	iconGhost = svgIcon(`<path d="M12 4a7 7 0 0 0-7 7v8l2.3-1.8L9.6 19 12 17.2 14.4 19l2.3-1.8L19 19v-8a7 7 0 0 0-7-7z"/><circle cx="9.5" cy="11" r="1" fill="currentColor"/><circle cx="14.5" cy="11" r="1" fill="currentColor"/>`)

	iconCollapse = svgIcon(`<rect x="4" y="7" width="16" height="10" rx="5"/><path d="M9 12h6"/>`)

	iconExternal = svgIcon(`<path d="M14 4h6v6"/><path d="M20 4l-9.5 9.5"/><path d="M18 13.5v4.5a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h4.5"/>`)

	iconSpotlight = svgIcon(`<circle cx="12" cy="12" r="5"/><path d="M12 2.5v2.5M12 19v2.5M2.5 12H5M19 12h2.5M5.3 5.3l1.8 1.8M16.9 16.9l1.8 1.8M18.7 5.3l-1.8 1.8M7.1 16.9l-1.8 1.8"/>`)

	iconErase = svgIcon(`<path d="M16.5 4.5l3 3a2 2 0 0 1 0 2.8L11 18.8a2 2 0 0 1-1.4.6H5.5L3.8 17.7a2 2 0 0 1 0-2.8L12.3 6.4l1.4-1.9a2 2 0 0 1 2.8 0z"/><path d="M8.5 11.5l4 4"/><path d="M11 19.4h9"/>`)

	iconShield = svgIcon(`<path d="M12 3l7.5 3.2v5.3c0 4.8-3.2 8.4-7.5 9.8-4.3-1.4-7.5-5-7.5-9.8V6.2L12 3z"/><path d="M9.5 12.2l1.8 1.8 3.5-3.6"/>`)

	iconTrash = svgIcon(`<path d="M4.5 6.5h15"/><path d="M9.5 6.5V4.5a1 1 0 0 1 1-1h3a1 1 0 0 1 1 1v2"/><path d="M6.5 6.5l1 13a1.5 1.5 0 0 0 1.5 1.4h6a1.5 1.5 0 0 0 1.5-1.4l1-13"/><path d="M10 10.5v6M14 10.5v6"/>`)

	iconScrollCap = svgIcon(`<rect x="5" y="3.5" width="14" height="17" rx="2"/><path d="M12 7.5v8.5"/><path d="M9 13.5l3 3 3-3"/><path d="M9 7.5h6"/>`)

	iconEdit = svgIcon(`<path d="M12 20h9"/><path d="M16.5 3.8a2.1 2.1 0 0 1 3 3L7 19.3 3 20.5l1.2-4L16.5 3.8z"/>`)

	iconDragFile = svgIcon(`<path d="M14 3.5H7A2 2 0 0 0 5 5.5v13A2 2 0 0 0 7 20.5h10a2 2 0 0 0 2-2V8.5L14 3.5z"/><path d="M14 3.5v5h5"/><path d="M9.5 14.5L12 12l2.5 2.5"/><path d="M12 12v5.5"/>`)

	iconHistory = svgIcon(`<path d="M3.5 12a8.5 8.5 0 1 0 2.5-6"/><path d="M3.5 3.5v3.8h3.8"/><path d="M12 7.5v5l3.2 2"/>`)

	iconSettings = svgIcon(`<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33h.08a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51h.08a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82v.08a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>`)

	iconTimer = svgIcon(`<circle cx="12" cy="13" r="7.5"/><path d="M10 3h4"/><path d="M12 9.5V13l2.5 1.8"/>`)

	iconFolder = svgIcon(`<path d="M3.5 6.5A1.5 1.5 0 0 1 5 5h4.2l2 2H19a1.5 1.5 0 0 1 1.5 1.5v9A1.5 1.5 0 0 1 19 19H5a1.5 1.5 0 0 1-1.5-1.5v-11z"/>`)

	iconCutout = svgIcon(`<circle cx="12" cy="8.5" r="3"/><path d="M6.8 18.5a5.5 5.5 0 0 1 10.4 0"/><path d="M4 7.5V5a1 1 0 0 1 1-1h2.5M20 7.5V5a1 1 0 0 0-1-1h-2.5M4 16.5V19a1 1 0 0 0 1 1h2.5M20 16.5V19a1 1 0 0 1-1 1h-2.5"/>`)

	iconLoupe = svgIcon(`<circle cx="10.8" cy="10.8" r="6.3"/><path d="M15.6 15.6l4.6 4.6"/><path d="M8.3 10.8h5M10.8 8.3v5"/>`)

	iconCompare = svgIcon(`<rect x="3.5" y="4.5" width="17" height="15" rx="2"/><path d="M12 2.5v19"/><path d="M8.5 9.5L6 12l2.5 2.5M15.5 9.5L18 12l-2.5 2.5"/>`)

	iconPalette = svgIcon(`<circle cx="12" cy="12" r="8.5"/><circle cx="8.8" cy="10.2" r="1.1" fill="currentColor"/><circle cx="12.6" cy="8.4" r="1.1" fill="currentColor"/><circle cx="15.5" cy="11.8" r="1.1" fill="currentColor"/><circle cx="11.5" cy="15.4" r="1.1" fill="currentColor"/>`)

	iconTemplate = svgIcon(`<rect x="3.5" y="4" width="17" height="16" rx="2.5"/><path d="M3.5 9.5h17"/><path d="M9.5 9.5v10.5"/>`)

	iconStickyNote = svgIcon(`<path d="M15.5 3.5H6a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2h8l5-5V5.5a2 2 0 0 0-2-2h-1.5z"/><path d="M14 20.5v-4.5a1 1 0 0 1 1-1h4.5"/><path d="M9.5 2.5v2.5M14.5 2.5v2.5"/>`)

	iconPolaroid = svgIcon(`<rect x="4" y="3.5" width="16" height="17" rx="1.8"/><rect x="6.5" y="6" width="11" height="9" rx="1"/><path d="M8.5 18h7"/>`)

	iconBrowser = svgIcon(`<rect x="3" y="4.5" width="18" height="15" rx="2.5"/><path d="M3 9h18"/><circle cx="6.2" cy="6.8" r="0.6" fill="currentColor"/><circle cx="8.8" cy="6.8" r="0.6" fill="currentColor"/><path d="M11.5 6.8h6.5"/>`)

	iconEditorial = svgIcon(`<rect x="4" y="3.5" width="16" height="17" rx="2.5"/><path d="M7.5 8h5"/><path d="M7.5 12h9v4.5h-9z"/>`)

	iconReceipt = svgIcon(`<path d="M6 3.5l2 1.8 2-1.8 2 1.8 2-1.8 2 1.8 2-1.8v17l-2-1.8-2 1.8-2-1.8-2 1.8-2-1.8-2 1.8v-17z"/><path d="M9 9h6M9 12.5h6M9 16h4"/>`)

	iconTerminal = svgIcon(`<rect x="3.5" y="4.5" width="17" height="15" rx="2.5"/><path d="M7.5 10l3 2.5-3 2.5"/><path d="M12.5 15h4"/>`)

	iconFilm = svgIcon(`<rect x="3.5" y="4.5" width="17" height="15" rx="2"/><path d="M3.5 8.5h17M3.5 15.5h17M7.5 4.5v4M12 4.5v4M16.5 4.5v4M7.5 15.5v4M12 15.5v4M16.5 15.5v4"/>`)

	iconReview = svgIcon(`<path d="M9 4.5h6a1.5 1.5 0 0 1 1.5 1.5v1h1.5A1.5 1.5 0 0 1 19.5 8.5v10a2 2 0 0 1-2 2h-11a2 2 0 0 1-2-2v-10A1.5 1.5 0 0 1 6 7h1.5V6A1.5 1.5 0 0 1 9 4.5z"/><path d="M8.5 13l2.2 2.2 4.8-4.8"/>`)

	iconPostcard = svgIcon(`<rect x="3" y="5" width="18" height="14" rx="2"/><rect x="14.5" y="7.5" width="4" height="4.5" rx="0.6"/><path d="M6.5 11.5h5.5M6.5 14.8h11"/>`)

	iconSlide = svgIcon(`<rect x="3" y="4.5" width="18" height="15" rx="2.2"/><path d="M10 4.5v15"/><path d="M5.8 9h2.2M5.8 12h2.5"/>`)

	iconUpdate = svgIcon(`<circle cx="12" cy="12" r="9"/><path d="M12 8v7.5"/><path d="M8.8 12.8L12 16l3.2-3.2"/>`)

	iconPause = svgIcon(`<path d="M9 5.5v13M15 5.5v13"/>`)

	iconPlay = svgIcon(`<polygon points="7.5,5.5 18.5,12 7.5,18.5"/>`)
)
