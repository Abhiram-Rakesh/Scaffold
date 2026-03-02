resource "random_id" "bucket_suffix" {
  byte_length = 4
}

resource "aws_s3_bucket" "sample" {
  bucket = "${var.project_name}-prod-${random_id.bucket_suffix.hex}"

  tags = {
    Name = "${var.project_name}-prod-sample"
  }
}

resource "aws_s3_bucket_versioning" "sample" {
  bucket = aws_s3_bucket.sample.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_public_access_block" "sample" {
  bucket                  = aws_s3_bucket.sample.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}
