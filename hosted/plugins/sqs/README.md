# SQS Ingester
This ingester reads messages off an [Amazon SQS](https://aws.amazon.com/sqs/) queue and writes them to Gravwell.
Each message is written as a single entry, timestamped using the queue's `SentTimestamp` system attribute when available.
Durability comes from SQS itself: messages are only deleted after they've been successfully written, so a crash or
write failure simply results in redelivery once the queue's visibility timeout elapses.

## AWS Docs
- [Amazon SQS](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/welcome.html)
- [Message System Attributes](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-message-metadata.html#sqs-system-attributes)
