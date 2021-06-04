#!/bin/bash

pip install pre-commit && \
  git submodule update --checkout --recursive && \
  pre-commit install

