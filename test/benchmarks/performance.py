import benchmark
import correctness
import os
import psutil
import shutil
import subprocess
import sys

benchmarking_dir = "benchmarks"
output_dir = "benchmark_results"

def get_pid_by_name(process_name: str) -> int:
    for proc in psutil.process_iter(["name"]):
        if proc.info["name"] == process_name:
            return proc.pid
    return -1

def setup() -> int:
    # download benchmarking repo
    repo_url = "https://github.com/cedana/cedana-benchmarks"
    subprocess.run(["git", "clone", repo_url, benchmarking_dir])

    # make folder for storing results
    os.makedirs(output_dir, exist_ok=True)

    return get_pid_by_name("cedana")

def cleanup():
    shutil.rmtree(benchmarking_dir)

def push_otel_to_bucket(filename, blob_id):
    client = storage.Client()
    bucket = client.bucket("benchmark-otel-data")
    blob = bucket.blob(blob_id)
    blob.upload_from_filename(filename)

def attach_bucket_id(csv_file, blob_id):
    # read csv file
    with open(csv_file, mode="r") as file:
        csv_reader = csv.reader(file)
        rows = list(csv_reader)

    # assume first row is the header containing column names
    header = rows[0]
    blob_id_column_index = header.index("blob_id")

    # update blob_id for each row
    for row in rows[1:]: # skip header row
        row[blob_id_column_index] = blob_id

    # write csv file
    with open(csv_file, mode="w", newline="") as file:
        csv_writer = csv.writer(file)
        csv_writer.writerows(rows)


def push_to_bigquery():
    client = bigquery.Client()

    dataset_id = "devtest"
    table_id = "benchmarks"

    csv_file_path = "benchmark_output.csv"

    job_config = LoadJobConfig(
        source_format=SourceFormat.CSV,
        skip_leading_rows=1, # change this according to your CSV file
        autodetect=True, # auto-detect schema if the table doesn't exist
        write_disposition="WRITE_APPEND", # options: WRITE_APPEND, WRITE_EMPTY, WRITE_TRUNCATE
    )

    dataset_ref = client.dataset(dataset_id)
    table_ref = dataset_ref.table(table_id)

    # API request to start the job
    with open(csv_file_path, "rb") as source_file:
        load_job = client.load_table_from_file(
            source_file, table_ref, job_config=job_config
        )

    load_job.result()

    if load_job.errors is not None:
        print("Errors:", load_job.errors)
    else:
        print("Job finished successfully.")

    # Get the table details
    table = client.get_table(table_ref)
    print("Loaded {} rows to {}".format(table.num_rows, table_id))

def main(args):
    daemon_pid = setup()
    if daemon_pid == -1:
        print("ERROR: cedana process not found in active PIDs. Have you started cedana daemon?")
        return

    if "--correctness" in args:
        blob_id = correctness.main(daemon_pid)
    else:
        blob_id = benchmark.main(daemon_pid)

    if not "--local" in args:
        from google.cloud import bigquery
        from google.cloud import storage
        from google.cloud.bigquery import LoadJobConfig, SourceFormat

        push_otel_to_bucket("/cedana/data.json", blob_id)
        attach_bucket_id("benchmark_output.csv", blob_id)
        push_to_bigquery()
        
    # delete benchmarking folder
    cleanup()

if __name__ == "__main__":
    main(sys.argv)
