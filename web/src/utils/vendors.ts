/**
 * 摄像头厂商的展示元数据。
 *
 * 说明：这里的图形是「品牌色 + 简化几何标识」的自制徽章，用于在列表中
 * 快速区分厂商，并非各厂商的注册商标文件。小米使用其标志性的橙色方块
 * 与 "MI" 字样，其余厂商用品牌色搭配字母或简化图形。
 */
export interface VendorMeta {
  /** 归一化厂商标识，与后端 Found.vendor 对应 */
  id: string
  /** 展示名 */
  name: string
  /** 品牌主色 */
  color: string
  /** 徽章内显示的文字；为空时使用几何图形 */
  text?: string
  /** 几何图形类型（未提供 text 时生效） */
  glyph?: 'flower' | 'eye' | 'camera' | 'rings'
}

export const VENDOR_UNKNOWN = 'unknown'

const VENDORS: Record<string, VendorMeta> = {
  hikvision: { id: 'hikvision', name: '海康威视', color: '#E4002B', glyph: 'eye' },
  dahua: { id: 'dahua', name: '大华', color: '#0066B3', text: 'DH' },
  huawei: { id: 'huawei', name: '华为', color: '#CF0A2C', glyph: 'flower' },
  xiaomi: { id: 'xiaomi', name: '小米', color: '#FF6900', text: 'MI' },
  tplink: { id: 'tplink', name: 'TP-LINK', color: '#4ACBD6', glyph: 'rings' },
  uniview: { id: 'uniview', name: '宇视', color: '#E60012', text: 'UNV' },
  tiandy: { id: 'tiandy', name: '天地伟业', color: '#0068B7', text: 'TD' },
  ezviz: { id: 'ezviz', name: '萤石', color: '#00A0E9', text: 'EZ' },
  vivotek: { id: 'vivotek', name: '晶睿', color: '#00857D', text: 'VIV' },
  axis: { id: 'axis', name: 'Axis', color: '#FFCC33', text: 'AXIS' },
  bosch: { id: 'bosch', name: '博世', color: '#EA0016', glyph: 'rings' },
  hanwha: { id: 'hanwha', name: '韩华', color: '#F37321', text: 'HW' },
  reolink: { id: 'reolink', name: 'Reolink', color: '#1B9CFC', text: 'RL' },
  oem: { id: 'oem', name: '通用模组', color: '#8C8C8C', glyph: 'camera' },
  [VENDOR_UNKNOWN]: { id: VENDOR_UNKNOWN, name: '未知厂商', color: '#B0B0B0', glyph: 'camera' },
}

export function vendorMeta(id?: string): VendorMeta {
  if (id && VENDORS[id]) return VENDORS[id]
  return VENDORS[VENDOR_UNKNOWN]
}

/** 厂商判定依据的说明文字，用于悬浮提示。 */
export function vendorSourceLabel(source?: string): string {
  switch (source) {
    case 'onvif':
      return '厂商来自设备 ONVIF 自报信息'
    case 'oui':
      return '厂商由网卡 MAC 地址（IEEE OUI）判定'
    case 'xiaomi':
      return '厂商由 8554 端口特征与 RTSP 路径探测判定'
    default:
      return '未能判定厂商'
  }
}
