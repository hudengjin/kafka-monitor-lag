CREATE TABLE if not exists consumer_lag_logs (
    stats_date date comment '统计日期',
    batch_no varchar(20) NOT NULL comment '批次信息',
    topic varchar(1024) NOT NULL comment '主题',
    consumer_group varchar(1024) NOT NULL comment '消费者组',
    total_lag bigint  comment '总延迟',
    max_lag bigint comment '最大延迟',
    current_lag bigint  comment '当前延迟',
    avg_lag bigint  comment '最大延迟',
    partition_counts int  comment '分区数',
    is_alert boolean  comment '是否告警',
    threshold bigint  comment '阈值',
    record_time datetime comment '记录时间'
)
PRIMARY KEY (stats_date,batch_no,topic,consumer_group)
PARTITION BY date_trunc('month', stats_date)
DISTRIBUTED BY HASH (batch_no)
PROPERTIES (
    "partition_live_number" = "24",
    "enable_persistent_index" = "true"
);
