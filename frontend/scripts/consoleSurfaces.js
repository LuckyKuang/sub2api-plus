// Bind component fills and generated utilities at build time. Text, borders
// and DOM opacity remain untouched; clients do not scan their stylesheets.
const colorFunction = /^(?:rgba?|hsla?|hwb|lab|lch|oklab|oklch|color|color-mix)$/i
const namedColors = new Set(('aliceblue antiquewhite aqua aquamarine azure beige bisque black blanchedalmond blue blueviolet brown burlywood cadetblue chartreuse chocolate coral cornflowerblue cornsilk crimson cyan darkblue darkcyan darkgoldenrod darkgray darkgreen darkgrey darkkhaki darkmagenta darkolivegreen darkorange darkorchid darkred darksalmon darkseagreen darkslateblue darkslategray darkslategrey darkturquoise darkviolet deeppink deepskyblue dimgray dimgrey dodgerblue firebrick floralwhite forestgreen fuchsia gainsboro ghostwhite gold goldenrod gray green greenyellow grey honeydew hotpink indianred indigo ivory khaki lavender lavenderblush lawngreen lemonchiffon lightblue lightcoral lightcyan lightgoldenrodyellow lightgray lightgreen lightgrey lightpink lightsalmon lightseagreen lightskyblue lightslategray lightslategrey lightsteelblue lightyellow lime limegreen linen magenta maroon mediumaquamarine mediumblue mediumorchid mediumpurple mediumseagreen mediumslateblue mediumspringgreen mediumturquoise mediumvioletred midnightblue mintcream mistyrose moccasin navajowhite navy oldlace olive olivedrab orange orangered orchid palegoldenrod palegreen paleturquoise palevioletred papayawhip peachpuff peru pink plum powderblue purple rebeccapurple red rosybrown royalblue saddlebrown salmon sandybrown seagreen seashell sienna silver skyblue slateblue slategray slategrey snow springgreen steelblue tan teal thistle tomato turquoise violet wheat white whitesmoke yellow yellowgreen currentcolor').split(' '))
const mix = value => `color-mix(in srgb, ${value} calc(var(--console-card-alpha, 1) * 100%), transparent)`
const sharedAlpha = /--(?:surface-|console-card-alpha|record-(?:table-bg|row-bg|row-tint))/i
const protectedSelector = /::?-webkit-scrollbar|app-layout::?before|appearance-preview-(?:light|dark)|appearance-(?:accent-)?swatch|surface-opaque|text-gradient|bg-clip-text|\.console-data-mark(?![\w-])|\.(?:history-bar|legend-dot|health-(?:score\d+|healthy|warning|critical|unknown)|score-legend|navigation-progress-bar)(?![\w-])/

function quotedEnd(value, start) {
  const quote = value[start]
  let end = start + 1
  while (end < value.length) {
    if (value[end] === '\\') { end += 2; continue }
    if (value[end++] === quote) break
  }
  return end
}

function functionEnd(value, start) {
  let end = start + 1, depth = 1
  while (end < value.length && depth) {
    const char = value[end]
    if (char === '"' || char === "'") end = quotedEnd(value, end)
    else if (char === '\\') end += 2
    else if (value.startsWith('/*', end)) {
      const close = value.indexOf('*/', end + 2)
      end = close < 0 ? value.length : close + 2
    } else {
      if (char === '(') depth++
      else if (char === ')') depth--
      end++
    }
  }
  return depth ? -1 : end
}

function fadeColors(value) {
  let output = '', index = 0
  while (index < value.length) {
    const char = value[index]
    if (char === '"' || char === "'") {
      const end = quotedEnd(value, index)
      output += value.slice(index, end); index = end; continue
    }
    if (value.startsWith('/*', index)) {
      const close = value.indexOf('*/', index + 2)
      const end = close < 0 ? value.length : close + 2
      output += value.slice(index, end); index = end; continue
    }
    const hex = char === '#' && /^#(?:[\da-f]{8}|[\da-f]{6}|[\da-f]{4}|[\da-f]{3})(?![\w-])/i.exec(value.slice(index))
    if (hex) { output += mix(hex[0]); index += hex[0].length; continue }
    const identifier = /^[-_a-z][-_a-z\d]*/i.exec(value.slice(index))
    if (!identifier) { output += char; index++; continue }
    const name = identifier[0]
    const open = index + name.length
    if (value[open] !== '(') {
      output += namedColors.has(name.toLowerCase()) ? mix(name) : name
      index = open; continue
    }
    const end = functionEnd(value, open)
    if (end < 0) { output += value.slice(index); break }
    const original = value.slice(index, end)
    const inner = value.slice(open + 1, end - 1)
    if (colorFunction.test(name)) {
      if (original.includes('--console-card-alpha')) output += original
      else if (sharedAlpha.test(original)) {
        // A mix may combine an already faded surface with an independent fill.
        output += name.toLowerCase() === 'color-mix' ? `${name}(${fadeColors(inner)})` : original
      } else output += mix(original)
    } else if (/gradient$|^light-dark$/i.test(name)) output += `${name}(${fadeColors(inner)})`
    else output += original // In particular, URLs, CSS vars and quoted data.
    index = end
  }
  return output
}

export default function consoleSurfaces() {
  return {
    postcssPlugin: 'console-surface-transparency',
    OnceExit(root) {
      const preservedOverrides = new Map()
      root.walkDecls(decl => {
        if (!/^(?:background(?:-color|-image)?|--tw-gradient-(?:from|to|stops))$/.test(decl.prop)) return
        const rule = decl.parent
        const selector = rule.selector ?? ''
        if (protectedSelector.test(selector) || rule.nodes?.some(node => node.prop === 'background-clip' && node.value === 'text')) return
        const previous = decl.value
        if (/^background(?:-color)?$/.test(decl.prop) && /^var\(/.test(previous) && functionEnd(previous, previous.indexOf('(')) === previous.length && !sharedAlpha.test(previous) && !/--console-/.test(previous)) decl.value = mix(previous)
        else decl.value = fadeColors(previous)
        if (decl.value !== previous && selector) {
          // Standalone gradient utilities can also color text. Preserve their
          // original stops when combined with Tailwind's text clipping utility.
          let overrides = preservedOverrides.get(rule)
          if (!overrides) { overrides = new Map(); preservedOverrides.set(rule, overrides) }
          const markers = []
          if (decl.prop.startsWith('--tw-gradient-')) markers.push('bg-clip-text')
          // Progress fills and semantic data marks can share regular utility
          // colors without fading them along with the containing surface.
          if (/(?:bg|from|via|to)-/.test(selector)) markers.push('console-data-mark')
          for (const marker of markers) {
            let override = overrides.get(marker)
            if (!override) {
              override = rule.clone({ selector: `:is(${selector}).${marker}`, nodes: [] })
              overrides.set(marker, override)
            }
            override.append(decl.clone({ value: previous }))
          }
        }
      })
      for (const [rule, overrides] of preservedOverrides) for (const override of overrides.values()) rule.after(override)
    }
  }
}
