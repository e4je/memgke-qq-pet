import { cpSync, mkdirSync, readFileSync, rmSync, writeFileSync, existsSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { packageVersion } from './release-version.mjs'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const template = JSON.parse(readFileSync(join(root, 'mengka-plugin.json'), 'utf8'))
template.version = packageVersion(template.version, process.env.PLUGIN_RELEASE_TAG || '')
const targets = process.argv.slice(2)
if (!targets.length) targets.push('windows-amd64', 'linux-amd64', 'linux-arm64')
for (const target of targets) {
  if (!['windows-amd64', 'linux-amd64', 'linux-arm64'].includes(target)) throw new Error(`Unsupported target: ${target}`)
}
function run(command, args, cwd = root, env = process.env) {
  const result = spawnSync(command, args, { cwd, env, stdio: 'inherit', windowsHide: true })
  if (result.error || result.status !== 0) throw result.error || new Error(`${command} failed (${result.status})`)
}
// Invoke the local CLIs directly, without a platform-dependent shell.
const frontend = join(root, 'frontend')
run(process.execPath, ['node_modules/vue-tsc/bin/vue-tsc.js', '--noEmit'], frontend)
run(process.execPath, ['node_modules/vite/bin/vite.js', 'build'], frontend)
const embedded = join(root, 'backend/internal/server/plugin-dist')
rmSync(embedded, { recursive: true, force: true })
cpSync(join(frontend, 'dist'), embedded, { recursive: true })
const output = resolve(process.env.PLUGIN_OUTPUT_DIR || join(root, 'dist/plugins'))
mkdirSync(output, { recursive: true })
const checksums = []
for (const target of targets) {
  const [goos, goarch] = target.split('-')
  const stage = join(root, '.plugin-build', target)
  rmSync(stage, { recursive: true, force: true })
  mkdirSync(stage, { recursive: true })
  // A root-level entry has identical spelling before and after Windows path
  // normalization. Omit unused optional arrays in the source manifest.
  const entry = `qq-pet${goos === 'windows' ? '.exe' : ''}`
  const manifest = { ...template, runtime: { ...template.runtime, entry } }
  writeFileSync(join(stage, 'mengka-plugin.json'), JSON.stringify(manifest, null, 2) + '\n')
  for (const file of ['LICENSE']) {
    if (existsSync(join(root, file))) cpSync(join(root, file), join(stage, file))
  }
  run('go', ['build', '-tags', 'mengka', '-trimpath', '-ldflags', `-s -w -X qq-pet/backend/internal/config.buildVersion=${template.version}`, '-o', join(stage, entry), './cmd/server'], join(root, 'backend'), { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: '0' })
  const name = `${template.plugin_id}-${template.version}-${target}.zip`
  run('go', ['run', join(root, 'scripts/package.go'), stage, join(output, name)])
  checksums.push(`${createHash('sha256').update(readFileSync(join(output, name))).digest('hex')}  ${name}`)
  console.log(`Created ${join(output, name)}`)
}
writeFileSync(join(output, 'SHA256SUMS.txt'), checksums.join('\n') + '\n')
