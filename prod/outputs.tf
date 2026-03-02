output "sample_s3_bucket_name" {
  description = "Name of the sample S3 bucket"
  value       = aws_s3_bucket.sample.id
}

output "sample_s3_bucket_arn" {
  description = "ARN of the sample S3 bucket"
  value       = aws_s3_bucket.sample.arn
}
