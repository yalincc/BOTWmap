import nodemailer from 'nodemailer'

// EdgeOne Pages Node Function: POST /api/diag
// 环境变量（在 EdgeOne Pages 项目控制台 → 设置 → 环境变量填写）：
//   QQ_EMAIL     你的 QQ 邮箱（发件方 & 默认收件方）
//   QQ_AUTH_CODE QQ 邮箱 16 位 SMTP 授权码
//   QQ_TO        可选，指定收件邮箱（不填默认发给 QQ_EMAIL）

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
    const edition = String(body.edition || 'emu').slice(0, 16)
    const emu = String(body.emulator || '-').slice(0, 32)
    const note = String(body.note || '').trim().slice(0, 300)
    const info = String(body.info || '').slice(0, 8000)
    const diag = String(body.diag || '').slice(0, 4000)
    const statusJson = String(body.status || '').slice(0, 16000)
    const coreLog = String(body.coreLog || '').slice(0, 64000)
    const eventsLog = String(body.eventsLog || '').slice(0, 32000)

    if (!coreLog && !info && !statusJson) {
      return jsonResp({ ok: false, error: 'Empty payload' }, 400)
    }

    const subject = `[xnavi ${version}][${edition}/${emu}]${note ? ' ' + note.slice(0, 40) : ' 诊断日志'}`
    const text = [
      note ? `用户描述：\n${note}\n` : '',
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
    return jsonResp({ ok: true }, 200)
  } catch (err) {
    return jsonResp({ ok: false, error: err && err.message ? err.message : 'send failed' }, 500)
  }
}
