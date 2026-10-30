import workspace from './workspace'
import requestList from './requestList'
import featureExtensions from './featureExtensions'
import adminAccessKeys from './adminAccessKeys'
import codexCli from './codexCli'
import codexCliRuntime from './codexCliRuntime'

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
import billingRates from './billingRates'

export default {
  ...workspace,
  ...requestList,
  ...inspection,
  ...billingRates,
  ...featureExtensions,
  ...codexCli,
  ...codexCliRuntime,
  adminAccessKeys,

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
