// eslint-disable-next-line import/extensions
const baseConfig = require('../.eslintrc.js')
module.exports = {
  extends: '../.eslintrc.js',
  parserOptions: {
    ...baseConfig.parserOptions,
    project: [__dirname + '/tsconfig.json'],
  },
  overrides: baseConfig.overrides,
}
