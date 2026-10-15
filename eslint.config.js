import globals from 'globals'
import base from '@jakobmelchard/config/eslint'

export default [
  {
    ignores: [
      'internal/lists/ui/static/htmx.min.js',
      'capacitor/www/',
      'capacitor/android/',
      'capacitor/ios/',
      'bin/',
      'tmp/',
    ],
  },
  ...base,
  // The pages and service workers run in the browser; tests, scripts and build.js on Node.
  { files: ['internal/*/ui/static/**'], languageOptions: { globals: globals.browser } },
  { files: ['internal/*/ui/static/sw.js'], languageOptions: { globals: globals.serviceworker } },
  {
    files: ['internal/*/ui/test/**', 'scripts/**', 'capacitor/build.js'],
    languageOptions: { globals: globals.node },
  },
  // Playwright runs the scripts' page.evaluate callbacks in the browser.
  { files: ['scripts/**'], languageOptions: { globals: globals.browser } },
]
