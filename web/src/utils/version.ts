// 底部“关于”版本号的来源决策。
// App（安卓 WebView）与服务器是两条独立发布线：App 内应显示 App 自身版本，
// 浏览器/fnOS 桌面显示服务端 Web 版本；两者都取不到时隐藏整行。
export type VersionSource = 'app' | 'server' | 'none'

export interface AboutVersion {
  version: string
  source: VersionSource
}

export function aboutVersion(app?: string, server?: string): AboutVersion {
  if (app) return { version: app, source: 'app' }
  if (server) return { version: server, source: 'server' }
  return { version: '', source: 'none' }
}
