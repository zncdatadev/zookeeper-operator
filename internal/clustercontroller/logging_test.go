package clustercontroller

import (
	zkv1alpha1 "github.com/zncdata-labs/zookeeper-operator/api/v1alpha1"
	"testing"
)

func Test_logDataBuilder(t *testing.T) {
	logDataBuilder := &LogDataBuilder{
		cfg: &zkv1alpha1.RoleGroupSpec{
			Config: &zkv1alpha1.ConfigSpec{
				Logging: &zkv1alpha1.ContainerLoggingSpec{
					Zookeeper: &zkv1alpha1.LoggingConfigSpec{
						Loggers: map[string]*zkv1alpha1.LogLevelSpec{
							"test1": {
								Level: "INFO",
							},
							"test2": {
								Level: "DEBUG",
							},
						},
						Console: &zkv1alpha1.LogLevelSpec{
							Level: "ERROR",
						},
						File: &zkv1alpha1.LogLevelSpec{
							Level: "WARN",
						},
					},
				},
			},
		},
	}
	type args struct {
		LogDataBuilder *LogDataBuilder
	}
	tests := []struct {
		name string
		args args
		want map[string]string
	}{
		// TODO: Add test cases.
		{
			name: "logback.xml content is correct",
			args: args{
				logDataBuilder,
			},
			want: wantData(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			//fmt.Println(tt.args.LogDataBuilder.MakeContainerLogData())
			if got := tt.args.LogDataBuilder.MakeContainerLogData(); !mapEq(got, tt.want) {
				t.Errorf("MakeCustomLogData() = %v, want %v", got, tt.want)
			}
		})
	}
}

// if map eq map
func mapEq(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func wantData() map[string]string {
	return map[string]string{
		"logback.xml": `<?xml version="1.0"?>
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
  <property name="zookeeper.console.threshold" value="ERROR"/>
<!--
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

  <!-- Add ROLLINGFILE appender if you want to use this -->
  <property name="zookeeper.log.dir" value="."/>
  <property name="zookeeper.log.file" value="zookeeper.log"/>
  <property name="zookeeper.log.threshold" value="WARN"/>
  <property name="zookeeper.log.maxfilesize" value="256MB"/>
  <property name="zookeeper.log.maxbackupindex" value="20"/>
<!--
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

  <root level="INFO">
    <appender-ref ref="CONSOLE"/>
    <!-- rolling file appender if you want to use this -->
    <appender-ref ref="ROLLINGFILE"/>
  </root>
  <!-- Add custom loggers if you want to use this -->
  <logger name="test1" level="INFO" />
<logger name="test2" level="DEBUG" />

</configuration>
`,
	}
}
