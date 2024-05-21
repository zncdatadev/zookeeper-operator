package clustercontroller

import (
	"fmt"

	zkv1alpha1 "github.com/zncdatadev/zookeeper-operator/api/v1alpha1"
	"github.com/zncdatadev/zookeeper-operator/internal/common"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewServerLogging(
	scheme *runtime.Scheme,
	instance *zkv1alpha1.ZookeeperCluster,
	client client.Client,
	groupName string,
	mergedLabels map[string]string,
	mergedCfg any,
	logDataBuilder common.RoleLoggingDataBuilder,
	role common.Role,
) *common.LoggingRecociler {
	return common.NewLoggingReconciler(scheme, instance, client, groupName, mergedLabels, mergedCfg, logDataBuilder,
		role)
}

type LogDataBuilder struct {
	cfg *zkv1alpha1.RoleGroupSpec
}

// MakeContainerLogData MakeContainerLog4jData implement RoleLoggingDataBuilder
func (c *LogDataBuilder) MakeContainerLogData() map[string]string {
	cfg := c.cfg
	data := make(map[string]string)
	// logger data
	if logging := cfg.Config.Logging; logging != nil {
		data[zkv1alpha1.LogbackFileName] = c.MakeLogbackData(logging.Zookeeper)
	}
	return data
}

// MakeCustomLogData make custom logger level data
const loggerTemplate = `<logger name="%s" level="%s" />`

func (c *LogDataBuilder) MakeCustomLogData(loggers map[string]*zkv1alpha1.LogLevelSpec) string {
	var lines string
	for logger, level := range loggers {
		loggerDefine := fmt.Sprintf(loggerTemplate, logger, level.Level)
		lines = lines + loggerDefine + "\n"
	}
	return lines
}

// make console appender data
const consoleAppenderPropertiesTemplate = `<property name="zookeeper.console.threshold" value="%s"/>` + "\n"

const consoleAppenderDefine = `<!--
    Add "console" to root logger if you want to use this
  -->
  <appender name="CONSOLE" class="ch.qos.logback.core.ConsoleAppender">
    <encoder>
      <pattern>%d{ISO8601} [myid:%X{myid}] - %-5p [%t:%C{1}@%L] - %m%n</pattern>
    </encoder>
    <filter class="ch.qos.logback.classic.filter.ThresholdFilter">
      <level>${zookeeper.console.threshold}</level>
    </filter>
  </appender>
`

func (c *LogDataBuilder) MakeConsoleAppenderData(consoleLevel *zkv1alpha1.LogLevelSpec) string {
	if consoleLevel != nil {
		return fmt.Sprintf(consoleAppenderPropertiesTemplate, consoleLevel.Level) + consoleAppenderDefine
	}
	return ""
}

// make file appender data
const fileAppenderPropertiesTemplate = `<property name="zookeeper.log.dir" value="/opt/bitnami/zookeeper/logs"/>
  <property name="zookeeper.log.file" value="zookeeper.log"/>
  <property name="zookeeper.log.threshold" value="%s"/>
  <property name="zookeeper.log.maxfilesize" value="256MB"/>
  <property name="zookeeper.log.maxbackupindex" value="20"/>
`
const fileAppenderDefine = `<!--
    Add ROLLINGFILE to root logger to get log file output
  -->
  <appender name="ROLLINGFILE" class="ch.qos.logback.core.rolling.RollingFileAppender">
    <File>${zookeeper.log.dir}/${zookeeper.log.file}</File>
    <encoder>
      <pattern>%d{ISO8601} [myid:%X{myid}] - %-5p [%t:%C{1}@%L] - %m%n</pattern>
    </encoder>
    <filter class="ch.qos.logback.classic.filter.ThresholdFilter">
      <level>${zookeeper.log.threshold}</level>
    </filter>
    <rollingPolicy class="ch.qos.logback.core.rolling.FixedWindowRollingPolicy">
      <maxIndex>${zookeeper.log.maxbackupindex}</maxIndex>
      <FileNamePattern>${zookeeper.log.dir}/${zookeeper.log.file}.%i</FileNamePattern>
    </rollingPolicy>
    <triggeringPolicy class="ch.qos.logback.core.rolling.SizeBasedTriggeringPolicy">
      <MaxFileSize>${zookeeper.log.maxfilesize}</MaxFileSize>
    </triggeringPolicy>
  </appender>
`

func (c *LogDataBuilder) MakeFileAppenderData(fileLevel *zkv1alpha1.LogLevelSpec) string {
	if fileLevel != nil {
		return fmt.Sprintf(fileAppenderPropertiesTemplate, fileLevel.Level) + fileAppenderDefine
	}
	return ""
}

// make logback xml data

const logbackTemplate = `<?xml version="1.0"?>
<!--
 Copyright 2022 The Apache Software Foundation

 Licensed to the Apache Software Foundation (ASF) under one
 or more contributor license agreements.  See the NOTICE file
 distributed with this work for additional information
 regarding copyright ownership.  The ASF licenses this file
 to you under the Apache License, Version 2.0 (the
 "License"); you may not use this file except in compliance
 with the License.  You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.

 Define some default values that can be overridden by system properties
-->
<configuration>
  <!-- Uncomment this if you would like to expose Logback JMX beans -->
  <!--jmxConfigurator /-->
  <!-- Add console appender if you want to use this -->
  %s
  <!-- Add ROLLINGFILE appender if you want to use this -->
  %s
  <root level="INFO">
    <appender-ref ref="CONSOLE"/>
    <!-- rolling file appender if you want to use this -->
    %s
  </root>
  <!-- Add custom loggers if you want to use this -->
  %s
</configuration>
`
const fileAppenderRefTemplate = `<appender-ref ref="%s"/>`

func (c *LogDataBuilder) MakeLogbackData(logSpec *zkv1alpha1.LoggingConfigSpec) string {
	var consoleAppenderDefine string
	var fileAppenderDefine string
	var fileAppenderRef string
	var customLoggerDefine string
	if console := logSpec.Console; console != nil {
		consoleAppenderDefine = c.MakeConsoleAppenderData(console)
	} else {
		consoleAppenderDefine = c.MakeConsoleAppenderData(&zkv1alpha1.LogLevelSpec{Level: "ERROR"})
	}
	if file := logSpec.File; file != nil {
		fileAppenderDefine = c.MakeFileAppenderData(file)
		fileAppenderRef = fmt.Sprintf(fileAppenderRefTemplate, "ROLLINGFILE")
	}
	if logSpec.Loggers != nil {
		customLoggerDefine = c.MakeCustomLogData(logSpec.Loggers)
	}
	return fmt.Sprintf(
		logbackTemplate,
		consoleAppenderDefine,
		fileAppenderDefine,
		fileAppenderRef,
		customLoggerDefine)
}
