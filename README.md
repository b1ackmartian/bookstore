# bookstore

## Container

### Build image (docker)

```sh
docker build -t bookstore:latest .
```

### Tag image for Digital Ocean registry

```sh
docker tag blog registry.digitalocean.com/at-docker/bookstore
```

### Push image to Digital Ocean registry

```sh
docker push registry.digitalocean.com/at-docker/bookstore
```

```sh
# login as posgres to create database, table and user
# this can be done through adminer as well
PGPASSWORD=$POSTGRES_PASSWORD psql --host 127.0.0.1 -U postgres -d postgres -p 5432
```

```sql
create database bookstore;
create user bookstoreuser with encrypted password 'bookstorepassword';
grant all privileges on database bookstore to bookstoreuser;
create table books (
    isbn char(14) NOT NULL,
    title varchar(255) NOT NULL,
    author varchar(255) NOT NULL,
    price decimal(5,2) NOT NULL
);
grant select, insert, update, delete on books to bookstoreuser;

alter table books owner to bookstoreuser;
alter table books add primary key (isbn);

insert into books (isbn, title, author, price) values
('978-1503261969', 'Emma', 'Jayne Austen', 9.44),
('978-1505255607', 'The Time Machine', 'H. G. Wells', 5.99),
('978-1503379640', 'The Prince', 'Niccolò Machiavelli', 6.99);
```
