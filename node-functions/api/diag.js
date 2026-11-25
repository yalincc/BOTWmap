import nodemailer from 'nodemailer'
import { createHash } from 'crypto'

// EdgeOne Pages Node Function: POST /api/diag
// 环境变量（在 EdgeOne Pages 项目控制台 → 设置 → 环境变量填写）：
//   QQ_EMAIL     你的 QQ 邮箱（发件方 & 默认收件方）
//   QQ_AUTH_CODE QQ 邮箱 16 位 SMTP 授权码
//   QQ_TO        可选，指定收件邮箱（不填默认发给 QQ_EMAIL）
//
// 2026-11-24：TOTK L1 配置单分享去重——云端已收录的链不再发邮件，直接回执感谢。
// 只影响 game=totk 且备注含 [分享本机 L1 配置] 的请求，其他诊断行为完全不变。

const PROFILES_URL = 'https://totk.yalin.site/profiles/index.json'

let transporter = null
function getTransporter(user, pass) {
  if (transporter) return transporter
  transporter = nodemailer.createTransport({
    host: 'smtp.qq.com',
    port: 465,
    secure: true,
    auth: { user, pass }
  })
  return transporter
}

function jsonResp(obj, status = 200) {
  return new Response(JSON.stringify(obj), {
    status,
    headers: { 'Content-Type': 'application/json; charset=utf-8' }
  })
}

// parseProfiles 解析 GUI 上传的 pcProfile 文本：多份 JSON 以空行 + // 注释分隔。
function parseProfiles(text) {
  const out = []
  for (const part of String(text || '').split(/\n{2,}(?=\/\/)/)) {
    const body = part.replace(/^\s*\/\/[^\n]*\n?/, '').trim()
    if (!body.startsWith('{')) continue
    try {
      const obj = JSON.parse(body)
      if (obj && Array.isArray(obj.staticChains) && obj.staticChains.length) out.push(obj)
    } catch {}
  }
  return out
}

// chainKeys 提取配置单内所有单条 L1 链的规范化键（大写 ptrOffset:structOffset，去重）。
function chainKeys(profile) {
  const seen = new Set()
  for (const c of (profile.staticChains || [])) {
    if (!c || !c.ptrOffset || !c.structOffset) continue
    seen.add(`${String(c.ptrOffset).trim().toUpperCase()}:${String(c.structOffset).trim().toUpperCase()}`)
  }
  return [...seen].sort()
}

// chainFingerprint 按整组链算指纹（用于统计云端已收录组数）。
function chainFingerprint(profile) {
  const keys = chainKeys(profile)
  if (!keys.length) return ''
  return createHash('sha256').update(keys.join(',')).digest('hex')
}

// fetchCloudProfiles 拉取云端已验证配置索引；失败返回 null（调用方降级发邮件兜底）。
async function fetchCloudProfiles() {
  const ctrl = new AbortController()
  const timer = setTimeout(() => ctrl.abort(), 4000)
  try {
    const res = await fetch(PROFILES_URL, { signal: ctrl.signal })
    if (!res.ok) return null
    const idx = await res.json()
    return Array.isArray(idx.profiles) ? idx.profiles : null
  } catch {
    return null
  } finally {
    clearTimeout(timer)
  }
}

export async function onRequestPost(context) {
  try {
    const env = context.env || process.env || {}
    const user = env.QQ_EMAIL
    const pass = env.QQ_AUTH_CODE
    if (!user || !pass) {
      return jsonResp({ ok: false, error: 'Missing QQ_EMAIL or QQ_AUTH_CODE' }, 500)
    }

    const body = await context.request.json()
    const version = String(body.version || 'unknown').slice(0, 32)
    const game = String(body.game || '').slice(0, 16)
    const edition = String(body.edition || 'emu').slice(0, 16)
    const emu = String(body.emulator || '-').slice(0, 32)
    const note = String(body.note || '').trim().slice(0, 300)
    const info = String(body.info || '').slice(0, 8000)
    const diag = String(body.diag || '').slice(0, 4000)
    const statusJson = String(body.status || '').slice(0, 16000)
    const pcProfile = String(body.pcProfile || '').slice(0, 32000)
    const coreLog = String(body.coreLog || '').slice(0, 64000)
    const eventsLog = String(body.eventsLog || '').slice(0, 32000)

    if (!coreLog && !info && !statusJson && !pcProfile) {
      return jsonResp({ ok: false, error: 'Empty payload' }, 400)
    }

    const isShareProfile = game === 'totk' && note.includes('[分享本机 L1 配置]')
    let dedupInfo = ''
    if (isShareProfile) {
      const submitted = parseProfiles(pcProfile)
      const cloud = await fetchCloudProfiles()
      if (cloud) {
        const knownGroups = new Set()
        const knownLinks = new Set()
        for (const p of cloud) {
          const fp = chainFingerprint(p)
          if (fp) knownGroups.add(fp)
          for (const k of chainKeys(p)) knownLinks.add(k)
        }
        // 单条链重合判定：同一游戏版本手动走动校准可能在前 12 条里有 1~2 条候选差异，
        // 只要命中 >=3 条已收录单链（短链则需全命中），即判定为同版本已收录，不再发重复邮件。
        const newOnes = submitted.filter(p => {
          const keys = chainKeys(p)
          if (!keys.length) return false
          const hits = keys.filter(k => knownLinks.has(k)).length
          return hits < Math.min(3, keys.length)
        })
        if (submitted.length > 0 && newOnes.length === 0) {
          return jsonResp({ ok: true, duplicate: true, msg: '该版本配置云端已收录，感谢支持' }, 200)
        }
        dedupInfo = [
          '=== L1 配置去重检查 ===',
          `云端配置：${cloud.length} 份 · 已收录链：${knownGroups.size} 组（${knownLinks.size} 条单链）`,
          `本次提交：${submitted.length} 份（新链 ${newOnes.length} 份）`,
          newOnes.length ? '提示：新链建议等 ≥2 份不同构建提交一致后再上线。' : '提示：未解析到有效链，请核对日志。',
          ''
        ].join('\n')
      } else {
        dedupInfo = '=== L1 配置去重检查 ===\n云端索引拉取失败，本次按新配置处理。\n\n'
      }
    }

    const subject = `${isShareProfile && !dedupInfo.includes('拉取失败') ? '[新 L1 配置]' : ''}[xnavi ${version}][${edition}/${emu}]${note ? ' ' + note.slice(0, 40) : ' 诊断日志'}`
    const text = [
      dedupInfo,
      note ? `用户描述：\n${note}\n` : '',
      pcProfile ? `=== pcProfile (L1 配置单) ===\n${pcProfile}\n` : '',
      `=== 环境自检 ===\n${diag || '(无)'}\n`,
      `=== 系统信息 ===\n${info || '(无)'}\n`,
      `=== status.json ===\n${statusJson || '(无)'}\n`,
      `=== run-events.log (tail) ===\n${eventsLog || '(无)'}\n`,
      `=== xnavi-gui-core.log (tail) ===\n${coreLog || '(无)'}\n`
    ].filter(Boolean).join('\n')

    const to = env.QQ_TO || user
    await getTransporter(user, pass).sendMail({
      from: `"Xnavi Diag" <${user}>`,
      to,
      subject,
      text
    })
    return jsonResp({ ok: true, duplicate: false }, 200)
  } catch (err) {
    return jsonResp({ ok: false, error: err && err.message ? err.message : 'send failed' }, 500)
  }
}
