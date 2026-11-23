export function applyQwenCacheSettings(credentials: Record<string, unknown>, sessionCache: boolean, strictAffinity: boolean, videoProtocol = ''): void {
  if (videoProtocol) credentials.qwen_video_protocol = videoProtocol
  else delete credentials.qwen_video_protocol
  for (const [key, enabled] of [
    ['qwen_session_cache_enabled', sessionCache],
    ['qwen_strict_session_affinity', strictAffinity]
  ] as const) {
    if (enabled) credentials[key] = true
    else delete credentials[key]
  }
}
