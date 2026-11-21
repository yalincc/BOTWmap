// Naive UI 的 useMessage 只能在 n-message-provider 的子组件 setup 里调用；
// 这里做一层桥接：MessageBridge.vue 在 provider 内挂接实例，其余模块直接 notify。
let messageApi = null

export function setMessageApi(m) {
  messageApi = m
}

export function notify(type, content) {
  if (messageApi && messageApi[type]) {
    messageApi[type](content)
    return
  }
  // provider 未就绪时兜底（不应发生）
  console.log(`[${type}]`, content)
}
