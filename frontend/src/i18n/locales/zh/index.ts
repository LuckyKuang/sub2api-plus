import requestList from './requestList'
import featureExtensions from './featureExtensions'
import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelMonitorV2 from './channelMonitorV2'
import channelMonitorV3 from './channelMonitorV3'
import batchImage from './batchImage'
import asyncImage from './asyncImage'
import admin from './admin'
import misc from './misc'
import inspection from './inspection'

export default {
  ...requestList,
  ...inspection,
  ...featureExtensions,
  ...landing,
  ...common,
  ...dashboard,
  ...channelMonitorV2,
  ...channelMonitorV3,
  ...batchImage,
  ...asyncImage,
  admin,
  ...misc,
}
