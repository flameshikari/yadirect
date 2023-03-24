FROM python:slim
COPY ./src/. /app
WORKDIR /app
CMD python main.py
