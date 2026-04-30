create table if not exists pbench_clusters
(
    cluster_name varchar(255) not null,
    cluster_fqdn varchar(255) not null,
    created      datetime(3)  not null,
    primary key (cluster_name, cluster_fqdn)
);
