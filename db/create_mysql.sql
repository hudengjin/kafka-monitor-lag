CREATE TABLE `consumer_lag_logs` (
  `stats_date` date NOT NULL COMMENT '统计日期',
  `batch_no` varchar(20) NOT NULL COMMENT '批次信息',
  `topic` varchar(128) NOT NULL COMMENT '主题',
  `consumer_group` varchar(128) NOT NULL COMMENT '消费者组',
  `total_lag` bigint DEFAULT NULL COMMENT '总延迟',
  `max_lag` bigint DEFAULT NULL COMMENT '最大延迟',
  `current_lag` bigint DEFAULT NULL COMMENT '当前延迟',
  `avg_lag` bigint DEFAULT NULL COMMENT '最大延迟',
  `partition_counts` int DEFAULT NULL COMMENT '分区数',
  `is_alert` tinyint(1) DEFAULT NULL COMMENT '是否告警',
  `threshold` bigint DEFAULT NULL COMMENT '阈值',
  `record_time` datetime DEFAULT NULL COMMENT '记录时间',
  PRIMARY KEY (`stats_date`,`batch_no`,`topic`,`consumer_group`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
