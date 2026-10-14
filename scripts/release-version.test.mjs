import test from 'node:test'
import assert from 'node:assert/strict'
import { packageVersion } from './release-version.mjs'

test('development keeps source version; releases derive version from their tag', () => {
  assert.equal(packageVersion('2.0.1'), '2.0.1')
  assert.equal(packageVersion('2.0.1', 'v2.0.1'), '2.0.1')
  assert.equal(packageVersion('2.0.1', '2.0.2'), '2.0.2')
  assert.equal(packageVersion('2.9.9', 'v2.10.0'), '2.10.0')
  assert.equal(packageVersion('2.9.9', 'v3.0.0'), '3.0.0')
})
test('reject downgrade tags and versions Mengka cannot import', () => {
  for (const tag of ['1.1.0', 'v2.0.0', 'v2.0.2-beta.1', 'main', 'v02.0.2', 'v2.0', 'v2.0.2\n', 'v2.0.2;echo hi']) {
    assert.throws(() => packageVersion('2.0.1', tag), undefined, tag)
  }
})
