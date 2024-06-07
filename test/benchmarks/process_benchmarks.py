import json
import pandas as pd
import os
import warnings
warnings.simplefilter(action='ignore', category=FutureWarning)

def fetch_aggs(benchmark_csv):
    df = pd.read_csv(benchmark_csv, index_col=False)
    df = df.rename(columns={
        'Job ID': 'Job_ID',
        'Operation Type': 'Operation_Type',
        'Memory Used Daemon': 'Memory_Used_Daemon',
        'CPU Utilization (Secs)': 'CPU_Utilization__Secs_',
        'CPU Used (Percent)': 'CPU_Used__Percent_',
        'Write (MB)': 'Write__MB_'
        })
    df['Job_ID'] = df['Job_ID'].str.replace(r'-\d+$', '', regex=True)
    aggs = df.groupby(['Job_ID', 'Operation_Type']).agg({
        'Memory_Used_Daemon': 'max',
        'CPU_Utilization__Secs_': 'max',
        'CPU_Used__Percent_': 'max',
        'Write__MB_': 'max'
    }).reset_index()
    return aggs

def load_and_process_json_objects(filename):
    data = []
    with open(filename, 'r') as file:
        for line in file:
            if line.strip(): # ensure the line is not empty
                try:
                    data.append(json.loads(line.strip())) # convert each line to dictionary
                except json.JSONDecodeError as e:
                    print(f"Error decoding JSON: {e}")
    return data

def flatten_spans(data):
    all_spans = {}
    parent_spans = {}

    for resource_span in data:
        for scope_span in resource_span['resourceSpans']:
            for span in scope_span['scopeSpans']:
                for individual_span in span['spans']:
                    # add span to all_spans dictionary
                    all_spans[individual_span['spanId']] = individual_span

                    # find 'jobID' within attributes and associate with parentSpanId if present
                    for attr in individual_span.get('attributes', []):
                        if attr['key'] == 'jobID':
                            parent_spans[individual_span['spanId']] = attr['value']['stringValue']
                            break

    return all_spans, parent_spans

def group_and_nest_spans(all_spans, parent_spans):
    # to hold grouped and nested spans
    grouped_data = {}

    # iterate over the span data, not the span IDs
    for span_id, span in all_spans.items():
        parent_span_id = span.get('parentSpanId')
        job_id = parent_spans.get(span_id) # retrieve jobID using current span's ID

        # if there's a parent span ID and it's in parent_spans, nest this span under the parent span
        if parent_span_id and parent_span_id in all_spans:
            parent_span = all_spans[parent_span_id]

            # ensure the parent span has a 'children' field and add this span to it
            if 'children' not in parent_span:
                parent_span['children'] = []
            parent_span['children'].append(span)

            # also group by jobID if available
            if job_id:
                if job_id not in grouped_data:
                    grouped_data[job_id] = {}
                if 'children' not in grouped_data[job_id]:
                    grouped_data[job_id]['children'] = []
                grouped_data[job_id]['children'].append(span)
        else:
            # for top-level spans, directly add them to the grouped_data under their jobID
            if job_id:
                if job_id not in grouped_data:
                    grouped_data[job_id] = {}
                grouped_data[job_id][span['name']] = span

    return grouped_data

def reduce_data(grouped_data):
    reduced_data = []

    for job_id, spans in grouped_data.items():
        for span_name, span_data in spans.items():
            # initialize a dictionary for the current span
            span_entry = {
                'Job_ID': job_id,
                'Operation_Type': span_name,
                'Duration': calculate_duration(span_data),
                'Attributes': span_data.get('attributes', {})
            }

            # if there are child spans, process them similarly
            if 'children' in span_data:
                for child in span_data['children']:
                    child_entry = {
                        'Name': child['name'],
                        'Duration': calculate_duration(child),
                        'Attributes': child.get('attributes', {})
                    }
                    # append child details to the span entry
                    span_entry.setdefault('Children', []).append(child_entry)

            reduced_data.append(span_entry)

    return reduced_data

def calculate_duration(span):
    start_time = int(span["startTimeUnixNano"])
    end_time = int(span["endTimeUnixNano"])
    return (end_time - start_time) / 1e9 # convert to seconds

def create_dataframe(reduced_data):
    data_for_df = []

    for span in reduced_data:
        row = {
            'Job_ID': span['Job_ID'],
            'Operation_Type': span['Operation_Type'],
            'Duration': span['Duration'],
            'Child_Spans': span.get('Children', []) # number of child spans
        }

        data_for_df.append(row)

    df = pd.DataFrame(data_for_df)
    df['Job_ID'] = df['Job_ID'].str.replace(r'-\d+$', '', regex=True)
    df['Operation_Type'] = df['Operation_Type'].str.replace(r'-ckpt', "", regex=True)
    df['Operation_Type'] = df['Operation_Type'].str.replace(r'dump', "checkpoint", regex=True)
    unique_rows = df.groupby(['Job_ID', 'Operation_Type'], as_index=False).first()
    unique_rows_columns = unique_rows[['Job_ID', 'Operation_Type', 'Child_Spans']]

    return unique_rows_columns

def push_to_bigquery(df: pd.DataFrame):
    from google.cloud import bigquery
    import pandas_gbq

    # serialize JSON column
    df["Child_Spans"] = df["Child_Spans"].apply(lambda x: json.dumps(x))

    project_id = "cedana-benchmarking"
    dataset_id = "devtest"
    table_id = "benchmark_data"
    try:
        gbq_tid = f"{dataset_id}.{table_id}" # gbq requires dataset_id.table_id format
        pandas_gbq.to_gbq(df, gbq_tid, project_id, if_exists="append", progress_bar=False)
    except Exception as e:
        print("Errors:", e)

    # Get the table details
    rows = df.shape[0]
    client = bigquery.Client()
    table_ref = client.dataset(dataset_id).table(table_id)
    table = client.get_table(table_ref)
    print("Loaded {} rows to {} (total: {} rows)".format(rows, table_id, table.num_rows))

def main(remote: bool, blob_id: str):
    filename = "data.json"
    if not os.path.exists(filename):
        filename = "/cedana/data.json"
    assert os.path.exists(filename)

    # process data.json
    json_objects = load_and_process_json_objects(filename)
    all_spans, parent_spans = flatten_spans(json_objects)
    grouped_data = group_and_nest_spans(all_spans, parent_spans)
    reduced_data = reduce_data(grouped_data)
    df = create_dataframe(reduced_data)

    # append w/ aggs
    aggs = fetch_aggs("benchmark_output.csv")
    result_df = pd.merge(aggs, df, on=['Job_ID', 'Operation_Type'], how='right')
    result_df['blob_id'] = blob_id
    result_df.to_csv("combined.csv", index=False)

    if remote:
        push_to_bigquery(result_df)

