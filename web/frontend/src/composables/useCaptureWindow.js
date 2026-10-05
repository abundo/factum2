// Opens the packet capture in its own browser window. With a device and
// interface, a second open brings that window to the front. Without them,
// each call is a new blank capture.
export function openCaptureWindow(deviceId, interfaceId) {
  const id = Number(deviceId)
  const iface = Number(interfaceId)
  const hasTarget = id > 0 && iface > 0
  const url = hasTarget
    ? `/capture/window?device=${encodeURIComponent(id)}&interface=${encodeURIComponent(iface)}`
    : '/capture/window'
  const name = hasTarget ? `factum-capture-${id}-${iface}` : `factum-capture-${Date.now()}`
  const w = window.open(url, name, 'popup,width=1280,height=860')
  w?.focus()
}
