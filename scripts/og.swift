#!/usr/bin/env swift
// Draws the sharing image, 1200 by 630: Espalier's icon and name in Allo over History, on the page's dark
// ground. scripts/images.sh runs it.
//
//     swift scripts/og.swift <AlloVariable-Round.ttf> <history.png> <icon.png> <out.png>

import AppKit
import CoreText

let args = CommandLine.arguments
guard args.count == 5 else {
    FileHandle.standardError.write(Data("usage: og.swift <AlloVariable-Round.ttf> <history.png> <icon.png> <out.png>\n".utf8))
    exit(2)
}
let fontURL = URL(fileURLWithPath: args[1])
guard let history = NSImage(contentsOfFile: args[2]), let icon = NSImage(contentsOfFile: args[3]) else {
    FileHandle.standardError.write(Data("og.swift: can't read the images\n".utf8))
    exit(1)
}
CTFontManagerRegisterFontsForURL(fontURL as CFURL, .process, nil)

/// Allo at a size and weight: Espalier's file, whose roundness is already at its roundest.
func allo(_ size: CGFloat, weight: CGFloat) -> NSFont {
    func tag(_ s: String) -> UInt32 { s.utf8.reduce(0) { ($0 << 8) | UInt32($1) } }
    let descriptor = CTFontDescriptorCreateWithAttributes([
        kCTFontNameAttribute: "AlloVariable-Regular",
        kCTFontVariationAttribute: [tag("wght"): weight, tag("opsz"): min(max(size, 9), 32)] as [UInt32: CGFloat],
    ] as CFDictionary)
    let font = CTFontCreateWithFontDescriptor(descriptor, size, nil) as NSFont
    // Core Text hands back another font when it can't find the one named: stop instead of drawing in that.
    guard font.fontName.hasPrefix("Allo") else {
        FileHandle.standardError.write(Data("og.swift: can't load Allo from \(fontURL.path)\n".utf8))
        exit(1)
    }
    return font
}
func color(_ hex: UInt32, _ alpha: CGFloat = 1) -> NSColor {
    NSColor(srgbRed: CGFloat((hex >> 16) & 0xFF) / 255, green: CGFloat((hex >> 8) & 0xFF) / 255,
            blue: CGFloat(hex & 0xFF) / 255, alpha: alpha)
}

let size = CGSize(width: 1200, height: 630)
let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: Int(size.width), pixelsHigh: Int(size.height),
                           bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
                           colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
NSGraphicsContext.saveGraphicsState()
let context = NSGraphicsContext(bitmapImageRep: rep)!
NSGraphicsContext.current = context
let cg = context.cgContext
// y runs down from the top.
cg.translateBy(x: 0, y: size.height)
cg.scaleBy(x: 1, y: -1)
NSGraphicsContext.current = NSGraphicsContext(cgContext: cg, flipped: true)
cg.interpolationQuality = .high

// Ground: the page's dark background.
color(0x1A1410).setFill()
CGRect(origin: .zero, size: size).fill()

// History, in its window, running off the bottom.
history.draw(in: CGRect(x: 80, y: 250, width: 1040, height: 1040 * history.size.height / history.size.width))

// The icon, then the name and what it is, each set by its baseline.
icon.draw(in: CGRect(x: 80, y: 64, width: 132, height: 132))
func draw(_ text: String, font: NSFont, color: NSColor, kern: CGFloat = 0, x: CGFloat, baseline: CGFloat) {
    NSAttributedString(string: text, attributes: [.font: font, .foregroundColor: color, .kern: kern])
        .draw(at: CGPoint(x: x, y: baseline - font.ascender))
}
draw("Espalier", font: allo(78, weight: 750), color: color(0xFFF9EE), kern: -78 * 0.035, x: 236, baseline: 140)
draw("A native Git client for the Mac", font: allo(29, weight: 400), color: color(0xC9BBA8), x: 240, baseline: 186)

NSGraphicsContext.restoreGraphicsState()
try rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: args[4]))
