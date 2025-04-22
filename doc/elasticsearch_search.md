# Elasticsearch Integration for Mattermost Search

This document explains how to configure and use Elasticsearch with Mattermost to achieve scalable search capabilities that can handle millions of messages efficiently.

## Overview

The default SQL-based search in Mattermost can become a performance bottleneck when the message volume grows beyond 2-3 million messages. The Elasticsearch integration solves this problem by providing:

1. **Scalable search performance** - Efficiently handles tens of millions of messages
2. **Advanced search capabilities** - Better relevance ranking and highlighting
3. **Reduced database load** - Offloads search operations from your primary database

## Requirements

- Elasticsearch v7.x or v8.x (recommended)
- Alternatively, OpenSearch v1.x or v2.x is also supported
- Mattermost Server v7.0 or higher

## Installation

### Step 1: Set up Elasticsearch

First, you need to have a running Elasticsearch or OpenSearch instance. You can:

- Install Elasticsearch directly on a server
- Use Docker to run Elasticsearch
- Use a managed Elasticsearch service (AWS, Google Cloud, Elastic Cloud, etc.)

Example Docker command:

```bash
docker run -d -p 9200:9200 -p 9300:9300 -e "discovery.type=single-node" -e "xpack.security.enabled=false" docker.elastic.co/elasticsearch/elasticsearch:8.9.1
```

### Step 2: Configure Mattermost

Edit your `config.json` file or use the System Console to configure Elasticsearch:

```json
"ElasticsearchSettings": {
    "ConnectionUrl": "http://localhost:9200",
    "Username": "",
    "Password": "",
    "EnableIndexing": true,
    "EnableSearching": true,
    "EnableAutocomplete": true,
    "Sniff": true,
    "IndexPrefix": "mattermost",
    "LiveIndexingBatchSize": 1,
    "BatchSize": 10000,
    "RequestTimeoutSeconds": 30
}
```

Configuration details:

| Setting | Description |
|---------|-------------|
| ConnectionUrl | URL of your Elasticsearch server |
| Username | Username if authentication is enabled |
| Password | Password if authentication is enabled |
| EnableIndexing | Enables Elasticsearch indexing |
| EnableSearching | Routes search queries to Elasticsearch |
| EnableAutocomplete | Uses Elasticsearch for autocomplete suggestions |
| Sniff | Automatically finds nodes in your ES cluster |
| IndexPrefix | Prefix for Elasticsearch indices |
| LiveIndexingBatchSize | Batch size for real-time indexing |
| RequestTimeoutSeconds | Timeout for Elasticsearch requests |

### Step 3: Create Initial Index

After configuring Elasticsearch, you need to create the initial index:

1. Navigate to **System Console > Environment > Elasticsearch**
2. Click on **Purge Indexes** button to create the initial index structure
3. Then click on **Index Now** to index existing messages

For large message volumes, indexing might take some time. You can monitor the progress in the System Console.

## Performance Considerations

### Elasticsearch Cluster Size

The required Elasticsearch cluster size depends on your message volume:

| Message Count | Recommended Nodes | RAM per Node | CPU per Node |
|---------------|-------------------|--------------|--------------|
| < 5 million   | 1                 | 8 GB         | 2 cores      |
| 5-20 million  | 3                 | 16 GB        | 4 cores      |
| > 20 million  | 5+                | 32 GB        | 8 cores      |

### Index Optimization

For large deployments, consider these settings:

```json
"ElasticsearchSettings": {
    "IndexReplicas": 1,
    "IndexShards": 3,
    "AggregatePostsAfterDays": 30
}
```

- `IndexReplicas`: Number of replica shards (1 for most deployments)
- `IndexShards`: Number of primary shards (increase for very large deployments)
- `AggregatePostsAfterDays`: Posts older than this will be aggregated into a separate index

## Testing Your Setup

1. After enabling Elasticsearch, perform a search in Mattermost
2. Verify the search returns expected results
3. Check Elasticsearch logs to ensure queries are being routed correctly

You can use the load test tool included with Mattermost to benchmark search performance:

```bash
cd mattermost/server
go test -v ./load_test -run TestElasticsearchPerformance
```

## Troubleshooting

### Common Issues

1. **Connection Problems**:
   - Verify the Elasticsearch server is running
   - Check network connectivity and firewall settings
   - Validate authentication credentials

2. **Search Returns No Results**:
   - Check if indexing is complete
   - Verify the index exists in Elasticsearch
   - Look for indexing errors in the Mattermost logs

3. **Slow Performance**:
   - Check Elasticsearch CPU and memory usage
   - Consider increasing the cluster size
   - Optimize the JVM heap size

### Viewing Logs

For detailed logging, set the following in your `config.json`:

```json
"ElasticsearchSettings": {
    "Trace": "error"
}
```

Valid values for `Trace` are: `error`, `info`, `debug`.

## Conclusion

With Elasticsearch integration, Mattermost search can scale efficiently to tens of millions of messages. The integration provides better search relevance and performance, making it suitable for enterprise deployments with large message volumes.

For additional help, refer to the [official Mattermost documentation](https://docs.mattermost.com/) or the [Elasticsearch documentation](https://www.elastic.co/guide/index.html). 