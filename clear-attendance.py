"""
Clears ONLY attendance records from the STAGING table.
Keeps all user profiles, projects, tasks, comments, day-offs, etc.

Items deleted: PK=USER#*, SK=ATTENDANCE#*
"""
import boto3

TABLE_NAME = "TaskManagementTable-staging"
REGION = "ap-south-1"

dynamodb = boto3.resource("dynamodb", region_name=REGION)
table = dynamodb.Table(TABLE_NAME)

print(f"Scanning {TABLE_NAME} for attendance records...")

# Scan for all attendance items (SK begins with ATTENDANCE#)
response = table.scan(
    FilterExpression="begins_with(SK, :sk)",
    ExpressionAttributeValues={":sk": "ATTENDANCE#"},
    ProjectionExpression="PK, SK",
)

items = response.get("Items", [])
while "LastEvaluatedKey" in response:
    response = table.scan(
        FilterExpression="begins_with(SK, :sk)",
        ExpressionAttributeValues={":sk": "ATTENDANCE#"},
        ProjectionExpression="PK, SK",
        ExclusiveStartKey=response["LastEvaluatedKey"],
    )
    items.extend(response.get("Items", []))

print(f"Found {len(items)} attendance records to delete.")

if not items:
    print("Nothing to delete.")
    exit(0)

# Show what will be deleted
for item in items[:5]:
    print(f"  {item['PK']} | {item['SK']}")
if len(items) > 5:
    print(f"  ... and {len(items) - 5} more")

confirm = input(f"\nDelete all {len(items)} attendance records from {TABLE_NAME}? (yes/no): ")
if confirm.strip().lower() != "yes":
    print("Cancelled.")
    exit(0)

# Batch delete
deleted = 0
with table.batch_writer() as batch:
    for item in items:
        batch.delete_item(Key={"PK": item["PK"], "SK": item["SK"]})
        deleted += 1

print(f"Deleted {deleted} attendance records from {TABLE_NAME}.")
print("User credentials and all other data are untouched.")
