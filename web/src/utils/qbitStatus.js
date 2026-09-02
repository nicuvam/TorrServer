import axios from 'axios'

import { settingsHost } from './Hosts'

let qbitEnabledPromise = null

export const clearQBitStatusCache = () => {
  qbitEnabledPromise = null
}

export const isQBitEnabled = () => {
  if (!qbitEnabledPromise) {
    qbitEnabledPromise = axios
      .post(settingsHost(), { action: 'get' })
      .then(({ data }) => Boolean(data?.QBitSettings?.Enabled && data?.QBitSettings?.URL))
      .catch(() => {
        qbitEnabledPromise = null
        return false
      })
  }

  return qbitEnabledPromise
}
