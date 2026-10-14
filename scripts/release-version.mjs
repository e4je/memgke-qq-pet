// Mengka accepts numeric X.Y.Z versions, without a prerelease suffix.
export function packageVersion(sourceVersion, releaseTag = '') {
  const parse = value => {
    if (value !== value.trim() || !/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(value)) {
      throw new Error(`Invalid Mengka version: ${value}; use X.Y.Z or a vX.Y.Z release tag`)
    }
    return value.split('.').map(BigInt)
  }
  const source = parse(sourceVersion)
  if (!releaseTag) return sourceVersion
  const version = releaseTag.replace(/^v/, '')
  const target = parse(version)
  for (let i = 0; i < 3; i++) {
    if (target[i] < source[i]) throw new Error(`Release ${releaseTag} is older than source version ${sourceVersion}; Mengka rejects downgrades`)
    if (target[i] > source[i]) break
  }
  return version
}
