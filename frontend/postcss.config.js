import tailwindcss from 'tailwindcss'
import autoprefixer from 'autoprefixer'
import consoleSurfaces from './scripts/consoleSurfaces.js'

export default { plugins: [tailwindcss(), autoprefixer(), consoleSurfaces()] }
